package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	t "time"

	"github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/vainsark/monitoring/agents/ids"
	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	scnInterval      = 400
	TransmitInterval = 1
	MaxMetricBuff    = 10
	timePast         = 0
	sendDelta        = 0
	MetricsLen       = 9
	ServerIP         = "localhost"
	DiskName         = ids.StorageLinuxID
)

type diskstat struct {
	BytesWrite uint64
	BytesRead  uint64
}
type bytesstat struct {
	BytesSent uint64
	BytesRecv uint64
}

func updateOrAppendMetric(metrics []*pb.Metric, id int32, agentId int32, dataName string, data float64) []*pb.Metric {
	// Append a new metric to the metrics slice
	newMetric := &pb.Metric{
		Id:        id,
		AgentId:   agentId,
		DataName:  dataName,
		Data:      data,
		Timestamp: timestamppb.New(t.Now()),
	}
	return append(metrics, newMetric)
}
func readSensorData(filename string) (float64, float64, error) {

	filePath := filename
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, 0, err
	}

	fields := strings.Fields(string(data))
	readCount, _ := strconv.ParseFloat(fields[1], 64)
	writeCount, _ := strconv.ParseFloat(fields[3], 64)

	return readCount, writeCount, nil
}
func timetosend() bool {
	return timePast == 0 || ((timePast-sendDelta)%(scnInterval*TransmitInterval) == 0)
}
func main() {
	if len(os.Args) > 1 {
		ServerIP = os.Args[1]
	}
	log.Printf("Server IP: %s\n", ServerIP)

	// Initialize the gRPC connection
	conn, err := grpc.NewClient(ServerIP+":50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer conn.Close()

	// Create a new gRPC client
	client := pb.NewLoadMonitorClient(conn)

	metrics := &pb.Metrics{}

	//=======================================================
	// Get the initial network IO counters
	netCounts, err := net.IOCounters(false)
	if err != nil {
		log.Fatalf("Error while getting Network IO Counters: %v", err)
	}
	BytesStat := bytesstat{
		BytesSent: netCounts[0].BytesSent,
		BytesRecv: netCounts[0].BytesRecv,
	}
	//=======================================================
	// Get the initial disk IO counters
	diskCounts, err := disk.IOCounters(DiskName)
	if err != nil {
		log.Fatalf("Error while getting disk IO Counters: %v", err)
	}
	DiskStats := diskstat{
		BytesWrite: diskCounts[DiskName].WriteBytes,
		BytesRead:  diskCounts[DiskName].ReadBytes,
	}
	//=======================================================
	for {
		// MAIN LOOP
		fmt.Printf("============= Load Scan time: %ds ==============\n", timePast)
		start_time := t.Now()
		//============================= CPU Usage =============================
		cpuPercent, err := cpu.Percent(0, false)
		if err != nil {
			log.Fatalf("Error while getting CPU usage: %v", err)
		}
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.CPUID, ids.LoadID, "CPU Usage", cpuPercent[0])

		//============================= Memory Usage =============================
		vmStat, err := mem.VirtualMemory()
		if err != nil {
			log.Fatalf("Error while getting memory usage: %v", err)
		}
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.MemoryID, ids.LoadID, "Memory Usage", vmStat.UsedPercent)

		//============================= Network Statistics =============================
		netCounts, err := net.IOCounters(false)
		if err != nil {
			log.Fatalf("Error while getting Network IO Counters: %v", err)
		}
		kBsOut := float64(netCounts[0].BytesSent-BytesStat.BytesSent) / 1024 / float64(scnInterval)
		kBsIn := float64(netCounts[0].BytesRecv-BytesStat.BytesRecv) / 1024 / float64(scnInterval)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.NetworkID, ids.LoadID, "BytesOut", kBsOut)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.NetworkID, ids.LoadID, "BytesIn", kBsIn)
		fmt.Printf("Network - Sent: %v kB/s, Received: %v kB/s\n", kBsOut, kBsIn)
		BytesStat.BytesSent = netCounts[0].BytesSent
		BytesStat.BytesRecv = netCounts[0].BytesRecv

		//============================= Storage (Disk Usage) =============================
		diskUtil, err := disk.Usage("/")
		if err != nil {
			log.Fatalf("Error while getting disk usage: %v", err)
		}
		diskCounts, err := disk.IOCounters(DiskName)
		if err != nil {
			log.Fatalf("Error while getting disk IO Counters: %v", err)
		}
		kBsWrite := float64(diskCounts[DiskName].WriteBytes-DiskStats.BytesWrite) / 1024 / float64(scnInterval)
		kBsRead := float64(diskCounts[DiskName].ReadBytes-DiskStats.BytesRead) / 1024 / float64(scnInterval)
		DiskStats.BytesWrite = diskCounts[DiskName].WriteBytes
		DiskStats.BytesRead = diskCounts[DiskName].ReadBytes
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.DiskID, ids.LoadID, "BytesWrite", kBsWrite)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.DiskID, ids.LoadID, "BytesRead", kBsRead)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.DiskID, ids.LoadID, "Disk Usage", diskUtil.UsedPercent)
		fmt.Printf("Disk - Write: %v kB/s, Read: %v kB/s\n       Total Usage :%.2f%% \n", kBsWrite, kBsRead, diskUtil.UsedPercent)

		//============================= Sensoric Load =============================
		filename := "sensoric_sim/counters.txt"
		senseRead, senseWrite, err := readSensorData(filename)
		if err != nil {
			log.Fatalf("Error reading sensor data: %v", err)
		}
		fmt.Printf("sensoric Load - Reads: %v, Writes: %v\n", senseRead, senseWrite)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.SensoricID, ids.LoadID, "Sensoric Read", senseRead)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.SensoricID, ids.LoadID, "Sensoric Write", senseWrite)

		//==========================================================================
		// Print the metrics buffer length and trim if necessary
		fmt.Printf("metrics length: %v\n", len(metrics.Metrics))
		if len(metrics.Metrics) > MetricsLen*MaxMetricBuff {
			fmt.Printf("Trimming oldest metrics...\n")
			metrics.Metrics = metrics.Metrics[MetricsLen:] // Trim the oldest metrics
		}

		//================================= Sending Data =================================
		if timetosend() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*t.Second)
			resp, err := client.LoadData(ctx, metrics)
			cancel() // cancel the context after the call finishes

			if err != nil {
				log.Printf("Error while calling LoadData: %v", err)
			} else {
				log.Printf("Response from server: %v", resp)

				TransmitInterval = int(resp.TransMult) // Update transmit multiplier
				newScnInterval := int(resp.ScnFreq)
				if scnInterval != newScnInterval {
					scnInterval = newScnInterval       // Update scan interval
					sendDelta = timePast % scnInterval // Update send delta for correct sending timing
				}
				metrics = &pb.Metrics{} // Reset metrics after sending
			}
		}

		timePast += scnInterval
		// Compensate for the time taken to process the metrics
		stop_time := t.Since(start_time)
		fmt.Printf("Total time taken: %v\n", stop_time)
		delta_sleep := (t.Duration(scnInterval) * t.Millisecond) - stop_time
		t.Sleep(delta_sleep)

	}
}
