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
	scnInterval      = 2000
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

func prtMetrics(metrics *pb.Metrics) {
	fmt.Printf("DeviceId: %s\n", metrics.DeviceId)
	fmt.Printf("AgentId: %d\n", metrics.AgentId)
	for _, metric := range metrics.Metrics {
		fmt.Printf("  %s: ", metric.DataName)
		fmt.Printf("  Data: %.2f\n", metric.Data)
	}
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
		intervalSec := float64(scnInterval) / 1000
		fmt.Printf("============= Availability Scan time: %ds ==============\n", timePast/1000)
		start_time := t.Now()

		var wg sync.WaitGroup
		wg.Add(3)

		cpuChan := make(chan float64, 1)
		memChan := make(chan memstats, 1)
		netChan := make(chan dropstats, 1)

		prevDropStats := DropStats
		//============================= CPU Idle Time Percent =============================
		go func() {
			defer wg.Done()
			cmd := exec.Command("bash", "-c", "iostat -c 1 2 ")
			output, err := cmd.Output()
			if err != nil {
				log.Printf("Error running iostat command: %v", err)
				cpuChan <- 0
				return
			}
			// outputStr := string(output)
			fields := strings.Fields(string(output))
			idle_percent, _ := strconv.ParseFloat(fields[32], 64)
			cpuChan <- idle_percent
		}()

		//============================= Memory Swaps =============================
		go func() {
			defer wg.Done()
			swapStat, err := mem.SwapMemory()
			if err != nil {
				log.Printf("Error while getting memory swaps: %v", err)
				memChan <- memstats{}
				return
			}
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
				log.Printf("Error while getting network IO counters: %v", err)
				netChan <- dropstats{}
				return
			}
			newIn := float64(netStats[0].Dropin)
			newOut := float64(netStats[0].Dropout)
			deltaInTotal := newIn - prev.newdropins
			deltaOutTotal := newOut - prev.newdropouts
			deltain := deltaInTotal / intervalSec
			deltaout := deltaOutTotal / intervalSec
			netChan <- dropstats{
				dropins:     deltain,
				dropouts:    deltaout,
				newdropins:  newIn,
				newdropouts: newOut,
			}
			// fmt.Printf("Dropped In: %v Packets, Dropped Out: %v Packets\n", deltain, deltaout)

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
