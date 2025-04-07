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

const ()

var (
	scnInterval      = 5
	TransmitInterval = 1
	MetricBuff       = 0
)

func updateOrAppendMetric(metrics []*pb.Metric, id int32, agentId int32, dataName string, data float64) []*pb.Metric {
	if MetricBuff == 0 {
		for i, m := range metrics {
			if m.DataName == dataName {
				metrics[i].Data = data
				metrics[i].Timestamp = timestamppb.New(t.Now())
				return metrics
			}
		}
	}
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
func main() {

	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewLoadMonitorClient(conn)

	metrics := &pb.Metrics{}
	// scnInterval := 5
	// TransmitInterval := 1
	// MetricBuff := 0
	timePast := 0
	//=======================================================
	type bytesstat struct {
		BytesSent uint64
		BytesRecv uint64
	}
	BytesStat := bytesstat{}
	netStats, err := net.IOCounters(false)
	if err != nil {
		log.Fatalf("Error while getting Network IO Counters: %v", err)
	}
	BytesStat.BytesSent = netStats[0].BytesSent
	BytesStat.BytesRecv = netStats[0].BytesRecv
	//=======================================================
	type diskstat struct {
		BytesWrite uint64
		BytesRead  uint64
	}
	DiskStats := diskstat{}
	diskStat, err := disk.IOCounters("sda")
	if err != nil {
		log.Fatalf("Error while getting disk IO Counters: %v", err)
	}
	DiskStats.BytesWrite = diskStat["sda"].WriteBytes
	DiskStats.BytesRead = diskStat["sda"].ReadBytes

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
		// fmt.Printf("CPU Usage: %.2f%%\n", cpuPercent)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.CPUID, ids.LoadID, "CPU Usage", cpuPercent[0])

		//============================= Memory Usage =============================

		vmStat, err := mem.VirtualMemory()
		if err != nil {
			log.Fatalf("Error while getting memory usage: %v", err)
		}
		// fmt.Printf("Memory Usage: %.2f%% (Total: %v MB, Used: %v MB)\n", vmStat.UsedPercent, vmStat.Total/1024/1024, vmStat.Used/1024/1024)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.MemoryID, ids.LoadID, "Memory Usage", vmStat.UsedPercent)

		//============================= Network Statistics =============================
		netStats, err := net.IOCounters(false)
		if err != nil {
			log.Fatalf("Error while getting Network IO Counters: %v", err)
		}

		kBsOut := float64(netStats[0].BytesSent-BytesStat.BytesSent) / 1024 / float64(scnInterval)
		kBsIn := float64(netStats[0].BytesRecv-BytesStat.BytesRecv) / 1024 / float64(scnInterval)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.NetworkID, ids.LoadID, "BytesOut", kBsOut)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.NetworkID, ids.LoadID, "BytesIn", kBsIn)
		fmt.Printf("Network - Sent: %v kB/s, Received: %v kB/s\n", kBsOut, kBsIn)
		BytesStat.BytesSent = netStats[0].BytesSent
		BytesStat.BytesRecv = netStats[0].BytesRecv

		//============================= Storage (Disk Usage) =============================

		diskUtil, err := disk.Usage("/")
		if err != nil {
			log.Fatalf("Error while getting disk usage: %v", err)
		}
		diskStat, err := disk.IOCounters("sda")
		if err != nil {
			log.Fatalf("Error while getting disk IO Counters: %v", err)
		}
		kBsWrite := float64(diskStat["sda"].WriteBytes-DiskStats.BytesWrite) / 1024 / float64(scnInterval)
		kBsRead := float64(diskStat["sda"].ReadBytes-DiskStats.BytesRead) / 1024 / float64(scnInterval)
		DiskStats.BytesWrite = diskStat["sda"].WriteBytes
		DiskStats.BytesRead = diskStat["sda"].ReadBytes
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.DiskID, ids.LoadID, "BytesWrite", kBsWrite)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.DiskID, ids.LoadID, "BytesRead", kBsRead)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.DiskID, ids.LoadID, "Disk Usage", diskUtil.UsedPercent)
		fmt.Printf("Disk - Write: %v kB/s, Read: %v kB/s\n", kBsWrite, kBsRead)

		//============================= Sensoric Load =============================
		filename := "sensoric_sim/counters.txt"

		senseRead, senseWrite, err := readSensorData(filename)
		if err != nil {
			log.Fatalf("Error reading sensor data: %v", err)
		}
		fmt.Printf("sensoric Load - Read: %.2f, Write: %.2f\n", senseRead, senseWrite)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.SensoricID, ids.LoadID, "Sensoric Read", senseRead)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.SensoricID, ids.LoadID, "Sensoric Write", senseWrite)

		//==========================================================================
		if timePast == 0 || (timePast%(scnInterval*TransmitInterval) == 0) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*t.Second)
			resp, err := client.LoadData(ctx, metrics)
			cancel() // cancel the context after the call finishes
			if err != nil {
				log.Printf("Error while calling LoadData: %v", err)
			} else {
				log.Printf("Response from server: %v", resp)

				scnInterval = int(resp.ScnFreq)
				TransmitInterval = int(resp.TransMult)
				if TransmitInterval > 1 {
					MetricBuff = 1
					metrics = &pb.Metrics{}
				} else {
					MetricBuff = 0
					metrics = &pb.Metrics{}
				}
			}
		}

		timePast += scnInterval
		stop_time := t.Since(start_time)
		fmt.Printf("Total time taken: %v\n", stop_time)
		delta_sleep := (t.Duration(scnInterval) * t.Second) - stop_time
		t.Sleep(delta_sleep)

	}
}
