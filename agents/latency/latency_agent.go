package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os/exec"
	"strconv"
	"strings"
	t "time"

	"github.com/go-ping/ping"
	"github.com/vainsark/monitoring/agents/ids"
	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	numAccesses    = 1000000
	arraySizeBytes = 1024 * 1024 // 1MB buffer
)

func updateOrAppendMetric(metrics []*pb.Metric, id int32, agentId int32, dataName string, data float64) []*pb.Metric {
	for i, m := range metrics {
		if m.DataName == dataName {
			metrics[i].Data = data
			return metrics
		}
	}
	newMetric := &pb.Metric{
		Id:       id,
		AgentId:  agentId,
		DataName: dataName,
		Data:     data,
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
	// Initialize the gRPC client
	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewLoadMonitorClient(conn)

	//=======================================================
	metrics := &pb.Metrics{}
	scnInterval := 5
	timePast := 0

	for {
		// MAIN LOOP
		fmt.Printf("============= Latency Scan time: %ds ==============\n", timePast)
		start_time := t.Now()

		//============================= Memory Latency =============================

		latency := measureMemoryLatency()
		fmt.Printf("Average Memory Latency: %v\n", latency)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.MemoryID, ids.LatencyID, "Memory Latency", float64(latency))

		//============================= Storage (IOSTAT) =============================

		cmd := exec.Command("bash", "-c", "iostat -dx sda 2 2| awk 'NR>2 {print $6, $12}'")
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

		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.DiskID, ids.LatencyID, "read wait", r_wait)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.DiskID, ids.LatencyID, "write wait", w_wait)

		//============================= Network Statistics =============================

		// Run the pinger
		pinger, err := ping.NewPinger(defaultIP)
		if err != nil {
			log.Fatalf("Failed to initialize pinger: %v", err)
		}
		pinger.Count = 4
		pinger.SetPrivileged(true)
		err = pinger.Run()
		if err != nil {
			log.Fatalf("Ping failed: %v", err)
		}

		// Get the results of the pings
		stats := pinger.Statistics()
		AvgRtt := float64(t.Duration(stats.AvgRtt).Microseconds()) / 1000
		fmt.Printf("Ping Results: %.2f\n", AvgRtt)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.NetworkID, ids.LatencyID, "NetInterLaten", AvgRtt)

		//====================================================================================

		ctx, cancel := context.WithTimeout(context.Background(), 5*t.Second)
		resp, err := client.LoadData(ctx, metrics)
		cancel()

		if err != nil {
			log.Printf("Error while calling LoadData: %v", err)
		} else {
			log.Printf("Response from server: %v", resp)
		}

		// Compensate for lost time for correct 5s of sleep.
		timePast += scnInterval
		stop_time := t.Since(start_time)
		fmt.Printf("Total time taken: %v\n", stop_time)
		delta_sleep := (t.Duration(scnInterval) * t.Second) - stop_time
		t.Sleep(delta_sleep)

	}
}
