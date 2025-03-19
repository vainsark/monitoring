package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	t "time"

	"github.com/go-ping/ping"
	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	AgentID = 2
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

func main() {

	cmd := exec.Command("bash", "-c", "ip route | grep default")
	output, err := cmd.Output()
	if err != nil {
		log.Fatalf("Error running command: %v", err)
	}
	// Convert the output (bytes) to a string.
	outputStr := string(output)
	fields := strings.Fields(outputStr)
	defaultIP := fields[2]
	fmt.Println("Default IP:", defaultIP)

	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewLoadMonitorClient(conn)

	metrics := &pb.Metrics{}
	scnInterval := 5
	timePast := 0
	//=======================================================

	//=======================================================
	for {
		// MAIN LOOP
		fmt.Printf("============= Scan time: %ds ==============\n", timePast)

		// Memory Usage

		// Network Statistics
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
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 3, AgentID, "NetInterLaten", AvgRtt)

		// Storage (IOSTAT)

		//=======================================================
		cmd := exec.Command("bash", "-c", "iostat -dx sda | awk 'NR>2 {print $6, $12}'")
		output, err := cmd.Output()
		if err != nil {
			log.Fatalf("Error running command: %v", err)
		}
		// Convert the output (bytes) to a string.
		outputStr := string(output)
		fields := strings.Fields(outputStr)
		fmt.Println("avg read wait:", fields[2])
		fmt.Println("avg write wait:", fields[3])

		ctx, cancel := context.WithTimeout(context.Background(), 5*t.Second)
		resp, err := client.LoadData(ctx, metrics)
		cancel()

		if err != nil {
			log.Printf("Error while calling LoadData: %v", err)
		} else {
			log.Printf("Response from server: %v", resp)
		}

		timePast += scnInterval
		t.Sleep(t.Duration(scnInterval) * t.Second)

	}
}
