package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
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
	scnInterval      = 1000
	TransmitInterval = 1
	MaxMetricBuff    = 20
	timePast         = 0
	sendDelta        = 0
	MetricsLen       = 9
	ServerIP         = "localhost"
	DiskName         = ids.StorageLinuxID
	devID            = string(ids.DeviceId)
	agentId          = ids.LoadID
)

type bytesstat struct {
	BytesSent uint64
	BytesRecv uint64
}

type diskstat struct {
	BytesWrite uint64
	BytesRead  uint64
}

type NetResult struct {
	kBsOut  float64
	kBsIn   float64
	newSent uint64
	newRecv uint64
}

type DiskResult struct {
	kBsWrite  float64
	kBsRead   float64
	diskUsage float64
	newWrite  uint64
	newRead   uint64
}

type SensorResult struct {
	senseRead  float64
	senseWrite float64
}

var BytesStat bytesstat
var DiskStats diskstat

func updateOrAppendMetric(metrics []*pb.Metric, dataName string, data float64) []*pb.Metric {
	newMetric := &pb.Metric{
		DataName:  dataName,
		Data:      data,
		Timestamp: timestamppb.New(t.Now()),
	}
	return append(metrics, newMetric)
}

func prtMetrics(metrics *pb.Metrics) {
	fmt.Printf("DeviceId: %s\n", metrics.DeviceId)
	fmt.Printf("AgentId: %d\n", metrics.AgentId)
	for _, metric := range metrics.Metrics {
		fmt.Printf("  %s: ", metric.DataName)
		fmt.Printf("  Data: %.2f\n", metric.Data)
	}
}
func readSensorData(filename string) (float64, float64, error) {
	data, err := os.ReadFile(filename)
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

	initialNet, err := net.IOCounters(false)
	if err != nil {
		log.Fatalf("Error getting initial network IO counters: %v", err)
	}
	BytesStat = bytesstat{
		BytesSent: initialNet[0].BytesSent,
		BytesRecv: initialNet[0].BytesRecv,
	}

	initialDisk, err := disk.IOCounters(DiskName)
	if err != nil {
		log.Fatalf("Error getting initial disk IO counters: %v", err)
	}
	DiskStats = diskstat{
		BytesWrite: initialDisk[DiskName].WriteBytes,
		BytesRead:  initialDisk[DiskName].ReadBytes,
	}

	conn, err := grpc.NewClient(ServerIP+":50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer conn.Close()
	client := pb.NewLoadMonitorClient(conn)

	// Initialize the metrics
	// Set the device ID and agent ID
	metrics := &pb.Metrics{DeviceId: devID, AgentId: agentId}

	for {
		fmt.Printf("============= Load Scan time: %vs ==============\n", timePast/1000)
		start_time := t.Now()

		var wg sync.WaitGroup
		wg.Add(5)

		cpuChan := make(chan float64, 1)
		memChan := make(chan float64, 1)
		netChan := make(chan NetResult, 1)
		diskChan := make(chan DiskResult, 1)
		sensorChan := make(chan SensorResult, 1)

		prevBytesStat := BytesStat
		prevDiskStats := DiskStats

		go func() {
			defer wg.Done()
			cpuPercents, err := cpu.Percent(0, false)
			if err != nil || len(cpuPercents) == 0 {
				log.Printf("Error getting CPU usage: %v", err)
				cpuChan <- 0
				return
			}
			cpuChan <- cpuPercents[0]
		}()

		go func() {
			defer wg.Done()
			vmStat, err := mem.VirtualMemory()
			if err != nil {
				log.Printf("Error getting memory usage: %v", err)
				memChan <- 0
				return
			}
			memChan <- vmStat.UsedPercent
		}()

		go func(prev bytesstat) {
			defer wg.Done()
			netCounts, err := net.IOCounters(false)
			if err != nil {
				log.Printf("Error getting network IO counters: %v", err)
				netChan <- NetResult{}
				return
			}
			newSent := netCounts[0].BytesSent
			newRecv := netCounts[0].BytesRecv
			kBsOut := float64(newSent-prev.BytesSent) / float64(scnInterval) / 1024
			kBsIn := float64(newRecv-prev.BytesRecv) / float64(scnInterval) / 1024
			netChan <- NetResult{kBsOut, kBsIn, newSent, newRecv}
		}(prevBytesStat)

		go func(prev diskstat) {
			defer wg.Done()
			diskUtil, err := disk.Usage("/")
			if err != nil {
				log.Printf("Error getting disk usage: %v", err)
			}
			diskCounts, err := disk.IOCounters(DiskName)
			if err != nil {
				log.Printf("Error getting disk IO counters: %v", err)
				diskChan <- DiskResult{}
				return
			}
			newWrite := diskCounts[DiskName].WriteBytes
			newRead := diskCounts[DiskName].ReadBytes
			kBsWrite := float64(newWrite-prev.BytesWrite) / float64(scnInterval) / 1024
			kBsRead := float64(newRead-prev.BytesRead) / float64(scnInterval) / 1024
			diskChan <- DiskResult{kBsWrite, kBsRead, diskUtil.UsedPercent, newWrite, newRead}
		}(prevDiskStats)

		go func() {
			defer wg.Done()
			filename := "../../sensoric_sim/counters.txt"
			data, err := os.ReadFile(filename)
			if err != nil {
				log.Printf("Error reading sensor data: %v", err)
				sensorChan <- SensorResult{}
				return
			}

			fields := strings.Fields(string(data))
			senseRead, _ := strconv.ParseFloat(fields[1], 64)
			senseWrite, _ := strconv.ParseFloat(fields[3], 64)
			sensorChan <- SensorResult{senseRead, senseWrite}
		}()

		wg.Wait()

		cpuUsage := <-cpuChan
		memUsage := <-memChan
		netResult := <-netChan
		diskResult := <-diskChan
		sensorResult := <-sensorChan

		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "CPU Usage", cpuUsage)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "Memory Usage", memUsage)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "BytesOut", netResult.kBsOut)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "BytesIn", netResult.kBsIn)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "BytesWrite", diskResult.kBsWrite)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "BytesRead", diskResult.kBsRead)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "Disk Usage", diskResult.diskUsage)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "Sensoric Read", sensorResult.senseRead)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "Sensoric Write", sensorResult.senseWrite)

		BytesStat.BytesSent = netResult.newSent
		BytesStat.BytesRecv = netResult.newRecv
		DiskStats.BytesWrite = diskResult.newWrite
		DiskStats.BytesRead = diskResult.newRead

		if len(metrics.Metrics) > MetricsLen*MaxMetricBuff {
			metrics.Metrics = metrics.Metrics[MetricsLen:]
		}
		prtMetrics(metrics)
		if timetosend() {
			// Save current batch in a local variable.
			m := metrics

			go func(m *pb.Metrics) {
				start := t.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 5*t.Second)
				resp, err := client.LoadData(ctx, m)
				cancel()
				if err != nil {
					log.Printf("Error while asynchronously sending data: %v", err)
				} else {
					log.Printf("Asynchronous response: %v", resp)
					TransmitInterval = int(resp.TransMult)
					newScnInterval := int(resp.ScnFreq)
					if scnInterval != newScnInterval {
						scnInterval = newScnInterval
						sendDelta = timePast % scnInterval
					}
				}
				stop_time := t.Since(start)
				fmt.Printf("Asynchronous sending time: %v\n", stop_time)
			}(m)
			// Prepare a new batch.
			metrics = &pb.Metrics{DeviceId: devID, AgentId: agentId}
		}

		timePast += scnInterval
		stop_time := t.Since(start_time)
		fmt.Printf("Total time taken: %v\n", stop_time)
		delta_sleep := (t.Duration(scnInterval) * t.Millisecond) - stop_time
		t.Sleep(delta_sleep)
	}
}
