package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	t "time"

	"github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/vainsark/monitoring/agents/ids"
	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	scnInterval      = 5
	TransmitInterval = 1
	MaxMetricBuff    = 10
	timePast         = 0
	sendDelta        = 0
	MetricsLen       = 9
	ServerIP         = "localhost"
)

type dropstats struct {
	dropins  uint64
	dropouts uint64
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
func timetosend() bool {
	// Calculate the time since the last send and corrects for correct modulus
	return timePast == 0 || ((timePast-sendDelta)%(scnInterval*TransmitInterval) == 0)
}
func main() {
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

	metrics := &pb.Metrics{}
	//=======================================================

	netStats, err := net.IOCounters(false)
	if err != nil {
		log.Fatalf("Error while getting Network IO Counters: %v", err)
	}
	DropStats := dropstats{
		dropins:  netStats[0].Dropin,
		dropouts: netStats[0].Dropout,
	}
	//=======================================================

	for {
		fmt.Printf("============= Availability Scan time: %ds ==============\n", timePast)
		start_time := t.Now()

		//============================= CPU Idle Time Percent =============================
		cmd := exec.Command("bash", "-c", "iostat -c 2 1 ")
		output, err := cmd.Output()
		if err != nil {
			log.Fatalf("Error running iostat command: %v", err)
		}
		// outputStr := string(output)
		fields := strings.Fields(string(output))
		idle_percent, _ := strconv.ParseFloat(fields[19], 64)
		fmt.Printf("CPU Idle time percent: %v%% \n", idle_percent)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.CPUID, ids.AvailabilityID, "Idle Percent", idle_percent)

		//============================= Memory Swaps =============================
		swapStat, err := mem.SwapMemory()
		if err != nil {
			log.Fatalf("Error while getting memory swaps: %v", err)
		}
		fmt.Printf("Swap Usage: %.2f%% (Total: %v MB, Used: %v MB)\n", swapStat.UsedPercent, swapStat.Total/1024/1024, swapStat.Used/1024/1024)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.MemoryID, ids.AvailabilityID, "Memory Swaps", swapStat.UsedPercent)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.MemoryID, ids.AvailabilityID, "Memory Swaps MB", float64(swapStat.Used/1024/1024))

		//============================= Network Statistics =============================
		netStats, err := net.IOCounters(false)
		if err != nil {
			log.Fatalf("Error while getting network IO counters: %v", err)
		}
		deltain := netStats[0].Dropin - DropStats.dropins
		deltaout := netStats[0].Dropout - DropStats.dropouts
		DropStats.dropins = netStats[0].Dropin
		DropStats.dropouts = netStats[0].Dropout
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.NetworkID, ids.AvailabilityID, "DropsIn", float64(deltain))
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, ids.NetworkID, ids.AvailabilityID, "DropsOut", float64(deltaout))
		fmt.Printf("Dropped In: %v Packets, Dropped Out: %v Packets\n", deltain, deltaout)

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
		delta_sleep := (t.Duration(scnInterval) * t.Second) - stop_time
		t.Sleep(delta_sleep)

	}
}
