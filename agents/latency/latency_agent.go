package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	t "time"

	"github.com/go-ping/ping"
	"github.com/vainsark/monitoring/agents/ids"
	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	numAccesses    = 1000000
	arraySizeBytes = 1024 * 1024 // 1MB buffer
)

var (
	scnInterval      = 2000
	TransmitInterval = 1
	MaxMetricBuff    = 10
	timePast         = 0
	sendDelta        = 0
	MetricsLen       = 9
	ServerIP         = "localhost"
	DiskName         = ids.StorageLinuxID
	devID            = ids.DeviceId
	agentId          = ids.LoadID
)

func updateOrAppendMetric(metrics []*pb.Metric, dataName string, data float64) []*pb.Metric {
	// Append a new metric to the metrics slice
	newMetric := &pb.Metric{
		DataName:  dataName,
		Data:      data,
		Timestamp: timestamppb.New(t.Now()),
	}
	return append(metrics, newMetric)
}

func measureMemoryLatency() t.Duration {

	numElements := arraySizeBytes / 4
	buffer := make([]int32, numElements)
	// Initialize buffer
	for i := 0; i < numElements; i++ {
		buffer[i] = int32(i)
	}
	var total t.Duration
	for i := 0; i < numAccesses; i++ {
		index := rand.Intn(numElements)
		// Start timing
		start := t.Now()

		// Read access
		value := buffer[index]
		_ = value // prevent "value is never used" wining

		// End timing
		elapsed := t.Since(start)
		total += elapsed
	}

	avg := total / numAccesses
	return avg
}

func simulSenseLatency() t.Duration {
	// simulate reading 64bit from sensor register with I2C at 400 kHz
	minRT := 250 * t.Microsecond

	deviation := rand.NormFloat64() * 50
	if deviation < 0 {
		deviation = -deviation
	}
	devTime := t.Duration(deviation) * t.Microsecond

	return minRT + devTime
}
func timetosend() bool {
	return timePast == 0 || ((timePast-sendDelta)%(scnInterval*TransmitInterval) == 0)
}
func main() {
	// Getting the default IP address
	cmd := exec.Command("bash", "-c", "ip route | grep default")
	output, err := cmd.Output()
	if err != nil {
		log.Fatalf("Error running command: %v", err)
	}
	fields := strings.Fields(string(output))
	defaultIP := fields[2]
	fmt.Println("Default IP:", defaultIP)

	//=======================================================

	if len(os.Args) > 1 {
		ServerIP = os.Args[1]
	}
	log.Printf("Server IP: %s\n", ServerIP)
	// Initialize the gRPC client
	conn, err := grpc.NewClient(ServerIP+":50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewLoadMonitorClient(conn)

	//=======================================================
	// Initialize the metrics
	// Set the device ID and agent ID
	metrics := &pb.Metrics{DeviceId: devID, AgentId: agentId}

	for {
		// MAIN LOOP
		fmt.Printf("============= Latency Scan time: %ds ==============\n", timePast)
		start_time := t.Now()

		//============================= Memory Latency =============================

		latency := measureMemoryLatency()
		fmt.Printf("Average Memory Latency: %v\n", latency)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "Memory Latency", float64(latency))

		//============================= Storage (IOSTAT) =============================

		cmd := exec.Command("bash", "-c", "iostat -dx ", DiskName, "2 2| awk 'NR>2 {print $6, $12}'")
		output, err := cmd.Output()
		if err != nil {
			log.Fatalf("Error running iostat command: %v", err)
		}
		// Convert the output (bytes) to a string.
		outputStr := string(output)
		fields := strings.Fields(outputStr)
		r_wait, _ := strconv.ParseFloat(fields[6], 64)
		w_wait, _ := strconv.ParseFloat(fields[7], 64)
		fmt.Printf("Raw string for read: %s\n", fields[6])
		fmt.Printf("Raw string for write: %s\n", fields[7])
		fmt.Printf("avg read wait: %.2f ms\n", r_wait)
		fmt.Printf("avg write wait: %.2f ms\n", w_wait)

		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "read wait", r_wait)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "write wait", w_wait)

		//============================= Network Statistics =============================

		// Run the pinger
		pinger, err := ping.NewPinger(defaultIP)
		if err != nil {
			log.Fatalf("Failed to initialize pinger: %v", err)
		}
		pinger.Count = 1
		pinger.SetPrivileged(true)
		err = pinger.Run()
		if err != nil {
			log.Fatalf("Ping failed: %v", err)
		}

		// Get the results of the pings
		stats := pinger.Statistics()
		AvgRtt := float64(t.Duration(stats.AvgRtt).Microseconds()) / 1000
		fmt.Printf("Ping Results: %.2f\n", AvgRtt)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "NetInterLaten", AvgRtt)

		//============================= Sensoric Latency =============================

		senseRT := simulSenseLatency()
		fmt.Printf("Simulated Sensoric Latency: %v\n", senseRT)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "Sensor Latency", float64(senseRT)/1000)

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
		stop_time := t.Since(start_time)
		fmt.Printf("Total time taken: %v\n", stop_time)
		delta_sleep := (t.Duration(scnInterval) * t.Millisecond) - stop_time
		t.Sleep(delta_sleep)

	}
}
