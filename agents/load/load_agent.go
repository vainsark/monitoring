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
	scnInterval   = 2000 //scan interval in milliseconds
	TransmitMult  = 1    // multiplier for the scan interval
	MaxMetricBuff = 10   // max number of metrics to keep in the buffer
	timePast      = 0
	sendDelta     = 0
	MetricsLen    = 9             // number of metrics per iteration
	ServerIP      = ids.ServerIP  // default server IP
	DiskName      = ids.StorageID // name of the disk to monitor
	devID         = ids.DeviceId
	agentId       = ids.LoadID
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

func appendMetric(metrics []*pb.Metric, dataName string, data float64) []*pb.Metric {
	/*	Appends a new metric to the metrics slice.
		Prints the metric name and value. */
	newMetric := &pb.Metric{
		DataName:  dataName,
		Data:      data,
		Timestamp: timestamppb.New(t.Now()),
	}
	fmt.Printf("  %s: ", dataName)
	fmt.Printf(" %.2f\n", data)
	return append(metrics, newMetric)
}

func readSensorData(filename string) (float64, float64, error) {
	// Read the sensor data from the file
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
	/*	/ Calculate the time since the last send and corrects for correct modulus */
	return timePast == 0 || ((timePast-sendDelta)%(scnInterval*TransmitMult) == 0)
}

func main() {
	// Getting the default IP address
	if len(os.Args) > 1 {
		ServerIP = os.Args[1]
	}
	log.Printf("Server IP: %s\n", ServerIP)

	// Get the initial network IO counters
	initialNet, err := net.IOCounters(false)
	if err != nil {
		log.Fatalf("Error getting initial network IO counters: %v", err)
	}
	BytesStat := bytesstat{
		BytesSent: initialNet[0].BytesSent,
		BytesRecv: initialNet[0].BytesRecv,
	}
	// Get the initial disk IO counters
	initialDisk, err := disk.IOCounters(DiskName)
	if err != nil {
		log.Fatalf("Error getting initial disk IO counters: %v", err)
	}
	DiskStats := diskstat{
		BytesWrite: initialDisk[DiskName].WriteBytes,
		BytesRead:  initialDisk[DiskName].ReadBytes,
	}
	// New gRPC connection
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
		intervalSec := float64(scnInterval) / 1000

		fmt.Printf("============= Load Scan time: %vs ==============\n", timePast/1000)
		start_time := t.Now()

		var wg sync.WaitGroup
		wg.Add(5) // Number of goroutines to wait for
		// Create channels for each goroutine
		cpuChan := make(chan float64, 1)
		memChan := make(chan float64, 1)
		netChan := make(chan NetResult, 1)
		diskChan := make(chan DiskResult, 1)
		sensorChan := make(chan SensorResult, 1)

		prevBytesStat := BytesStat
		prevDiskStats := DiskStats
		//============================= CPU =============================
		go func() {
			// Get CPU usage
			defer wg.Done()
			cpuPercents, err := cpu.Percent(0, false)
			if err != nil || len(cpuPercents) == 0 {
				log.Printf("Error getting CPU usage: %v", err)
				cpuChan <- 0
				return
			}
			cpuChan <- cpuPercents[0]
		}()
		//============================= Memory =============================
		go func() {
			// Get memory usage
			defer wg.Done()
			vmStat, err := mem.VirtualMemory()
			if err != nil {
				log.Printf("Error getting memory usage: %v", err)
				memChan <- 0
				return
			}
			memChan <- vmStat.UsedPercent
		}()
		//============================= Network =============================
		go func(prev bytesstat) {
			// Get network IO counters
			defer wg.Done()
			netCounts, err := net.IOCounters(false)
			if err != nil {
				log.Printf("Error getting network IO counters: %v", err)
				netChan <- NetResult{}
				return
			}

			newSent := netCounts[0].BytesSent
			newRecv := netCounts[0].BytesRecv
			// Calculate the delta for sent and received bytes
			deltaSent := netCounts[0].BytesSent - prev.BytesSent
			deltaRecv := netCounts[0].BytesRecv - prev.BytesRecv
			// convert Bytes to kB/s
			kBsOut := float64(deltaSent) / 1000 / intervalSec
			kBsIn := float64(deltaRecv) / 1000 / intervalSec
			netChan <- NetResult{kBsOut, kBsIn, newSent, newRecv}
		}(prevBytesStat)
		//============================= Disk =============================
		go func(prev diskstat) {
			// Get disk usage
			defer wg.Done()
			// Get disk usage
			diskUtil, err := disk.Usage("/")
			if err != nil {
				log.Printf("Error getting disk usage: %v", err)
			}
			// Get disk IO counters
			diskCounts, err := disk.IOCounters(DiskName)
			if err != nil {
				log.Printf("Error getting disk IO counters: %v", err)
				diskChan <- DiskResult{}
				return
			}
			newWrite := diskCounts[DiskName].WriteBytes
			newRead := diskCounts[DiskName].ReadBytes
			// Calculate the delta for written and read bytes
			deltaWrite := newWrite - prev.BytesWrite
			deltaRead := newRead - prev.BytesRead
			// convert Bytes to kB/s
			kBsWrite := float64(deltaWrite) / 1000 / intervalSec
			kBsRead := float64(deltaRead) / 1000 / intervalSec
			diskChan <- DiskResult{kBsWrite, kBsRead, diskUtil.UsedPercent, newWrite, newRead}
		}(prevDiskStats)
		//============================= Sensoric =============================
		go func() {
			defer wg.Done()

			filename := "sensoric_sim/counters.txt"
			senseRead, senseWrite, err := readSensorData(filename)
			if err != nil {
				log.Printf("Error reading sensor data: %v", err)
				sensorChan <- SensorResult{}
				return
			}
			sensorChan <- SensorResult{senseRead, senseWrite}
		}()
		// Wait for all goroutines to finish
		wg.Wait()
		// Close the channels
		cpuUsage := <-cpuChan
		memUsage := <-memChan
		netResult := <-netChan
		diskResult := <-diskChan
		sensorResult := <-sensorChan
		// Append all metrics to the metrics slice
		metrics.Metrics = appendMetric(metrics.Metrics, "CPU Usage", cpuUsage)
		metrics.Metrics = appendMetric(metrics.Metrics, "Memory Usage", memUsage)
		metrics.Metrics = appendMetric(metrics.Metrics, "BytesOut", netResult.kBsOut)
		metrics.Metrics = appendMetric(metrics.Metrics, "BytesIn", netResult.kBsIn)
		metrics.Metrics = appendMetric(metrics.Metrics, "BytesWrite", diskResult.kBsWrite)
		metrics.Metrics = appendMetric(metrics.Metrics, "BytesRead", diskResult.kBsRead)
		metrics.Metrics = appendMetric(metrics.Metrics, "Disk Usage", diskResult.diskUsage)
		metrics.Metrics = appendMetric(metrics.Metrics, "Sensoric Read", sensorResult.senseRead)
		metrics.Metrics = appendMetric(metrics.Metrics, "Sensoric Write", sensorResult.senseWrite)

		// Set the new values for the next iteration
		BytesStat.BytesSent = netResult.newSent
		BytesStat.BytesRecv = netResult.newRecv
		DiskStats.BytesWrite = diskResult.newWrite
		DiskStats.BytesRead = diskResult.newRead
		//==========================================================================
		// Print the metrics buffer length and trim if necessary (only for debugging)
		if len(metrics.Metrics) > MetricsLen*MaxMetricBuff {
			metrics.Metrics = metrics.Metrics[MetricsLen:] // Trim the oldest metrics
		}

		if timetosend() {
			// Save current batch in a local variable.
			m := metrics
			// Reset the metrics for the next batch
			metrics = &pb.Metrics{DeviceId: devID, AgentId: agentId}
			go func(m *pb.Metrics) {
				/* Asynchronously send the data, this will not block the main thread
				Create a new context with a timeout */
				start := t.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 5*t.Second) // Set a timeout for the context
				resp, err := client.LoadData(ctx, m)                                 // Send the data to the server
				cancel()
				if err != nil {
					log.Printf("Error while asynchronously sending data: %v", err)
				} else {
					log.Printf("Asynchronous response: %v", resp)
					TransmitMult = int(resp.TransMult)
					newScnInterval := int(resp.ScnFreq)
					if scnInterval != newScnInterval {
						scnInterval = newScnInterval
						sendDelta = timePast % scnInterval // compensate for the time passed
					}
				}
				stop_time := t.Since(start)
				fmt.Printf("Asynchronous sending time: %v\n", stop_time)
			}(m)
		}

		timePast += scnInterval
		stop_time := t.Since(start_time) // Calculate the time taken for the scan
		fmt.Printf("Total time taken: %v\n", stop_time)
		// Calculate the actual sleep time (to account for processing time)
		delta_sleep := (t.Duration(scnInterval) * t.Millisecond) - stop_time
		t.Sleep(delta_sleep)
	}
}
