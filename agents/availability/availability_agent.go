package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
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
	devID            = ids.DeviceId
	agentId          = ids.AvailabilityID
)

type dropstats struct {
	dropins     float64
	dropouts    float64
	newdropins  float64
	newdropouts float64
}
type memstats struct {
	swaps   float64
	swapsMB float64
}

func updateOrAppendMetric(metrics []*pb.Metric, dataName string, data float64) []*pb.Metric {
	// Append a new metric to the metrics slice
	newMetric := &pb.Metric{
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
	// Set the server IP address from command line argument or use default
	if len(os.Args) > 1 {
		ServerIP = os.Args[1]
	}
	log.Printf("Server IP: %s\n", ServerIP)

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
	//=======================================================
	// Initialize the drop statistics
	netStats, err := net.IOCounters(false)
	if err != nil {
		log.Fatalf("Error while getting Network IO Counters: %v", err)
	}
	DropStats := dropstats{
		newdropins:  float64(netStats[0].Dropin),
		newdropouts: float64(netStats[0].Dropout),
	}
	//=======================================================

	for {

		fmt.Printf("============= Availability Scan time: %ds ==============\n", timePast)
		start_time := t.Now()
		var wg sync.WaitGroup
		wg.Add(5)

		cpuChan := make(chan float64, 1)
		memChan := make(chan memstats, 1)
		netChan := make(chan dropstats, 1)

		prevDropStats := DropStats
		//============================= CPU Idle Time Percent =============================
		go func() {
			defer wg.Done()
			cmd := exec.Command("bash", "-c", "iostat -c 2 1 ")
			output, err := cmd.Output()
			if err != nil {
				log.Fatalf("Error running iostat command: %v", err)
				cpuChan <- 0
				return
			}
			// outputStr := string(output)
			fields := strings.Fields(string(output))
			idle_percent, _ := strconv.ParseFloat(fields[19], 64)
			fmt.Printf("CPU Idle time percent: %v%% \n", idle_percent)
			cpuChan <- idle_percent
		}()

		//============================= Memory Swaps =============================
		go func() {
			defer wg.Done()
			swapStat, err := mem.SwapMemory()
			if err != nil {
				log.Fatalf("Error while getting memory swaps: %v", err)
				memChan <- memstats{}
				return
			}
			fmt.Printf("Swap Usage: %.2f%% (Total: %v MB, Used: %v MB)\n", swapStat.UsedPercent, swapStat.Total/1024/1024, swapStat.Used/1024/1024)

			memChan <- memstats{
				swaps:   swapStat.UsedPercent,
				swapsMB: float64(swapStat.Used / 1024 / 1024),
			}
		}()

		//============================= Network Statistics =============================
		go func(prev dropstats) {
			defer wg.Done()
			netStats, err := net.IOCounters(false)
			if err != nil {
				log.Fatalf("Error while getting network IO counters: %v", err)
				netChan <- dropstats{}
				return
			}
			newIn := float64(netStats[0].Dropin)
			newOut := float64(netStats[0].Dropout)
			deltain := newIn - prev.newdropins/float64(scnInterval)
			deltaout := newOut - prev.newdropouts/float64(scnInterval)

			netChan <- dropstats{
				dropins:     deltain,
				dropouts:    deltaout,
				newdropins:  newIn,
				newdropouts: newOut,
			}
			fmt.Printf("Dropped In: %v Packets, Dropped Out: %v Packets\n", deltain, deltaout)

		}(prevDropStats)

		wg.Wait()

		idle_percent := <-cpuChan
		swapStat := <-memChan

		deltadrops := <-netChan

		//==========================================================================
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "Idle Percent", idle_percent)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "Memory Swaps", swapStat.swaps)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "Memory Swaps MB", float64(swapStat.swapsMB))
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "DropsIn", float64(deltadrops.dropins))
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, "DropsOut", float64(deltadrops.dropouts))

		DropStats.dropins = deltadrops.dropins
		DropStats.dropouts = deltadrops.dropouts

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
