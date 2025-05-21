package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	t "time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/net"
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
	MetricsLen    = 9            // number of metrics per iteration
	ServerIP      = ids.ServerIP // default server IP
	devID         = ids.DeviceId
	agentId       = ids.AvailabilityID
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

func sumAll(t cpu.TimesStat) float64 {
	/*	Sums all the CPU times. */
	return t.User + t.System + t.Idle +
		t.Nice + t.Iowait + t.Irq +
		t.Softirq + t.Steal + t.Guest + t.GuestNice
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
func timetosend() bool {
	/*	/ Calculate the time since the last send and corrects for correct modulus */
	return timePast == 0 || ((timePast-sendDelta)%(scnInterval*TransmitMult) == 0)
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
	metrics := &pb.Metrics{DeviceId: devID, AgentId: agentId}
	//=======================================================
	// Initialize drop statistics
	netStats, err := net.IOCounters(false)
	if err != nil {
		log.Fatalf("Error while getting Network IO Counters: %v", err)
	}
	DropStats := dropstats{
		newdropins:  float64(netStats[0].Dropin),
		newdropouts: float64(netStats[0].Dropout),
	}
	//=======================================================
	// Initialize CPU times
	snap, err := cpu.Times(false)
	if err != nil {
		log.Fatalf("failed to prime CPU times: %v", err)
	}
	prevCPUTimes := snap[0]

	for {
		start_time := t.Now() // Start the timer for the scan
		intervalSec := float64(scnInterval) / 1000
		fmt.Printf("============= Availability Scan time: %ds ==============\n", timePast/1000)

		var wg sync.WaitGroup
		wg.Add(3) // Number of goroutines to wait for
		// Create channels for each goroutine
		cpuChan := make(chan float64, 1)
		memChan := make(chan memstats, 1)
		netChan := make(chan dropstats, 1)

		prevDropStats := DropStats
		//============================= CPU Availability Percent =============================
		go func() {
			defer wg.Done()
			times, err := cpu.Times(false)
			if err != nil {
				log.Printf("cpu.Times error: %v", err)
			}
			curr := times[0]
			// compute deltas
			deltaUser := curr.User - prevCPUTimes.User
			deltaIdle := curr.Idle - prevCPUTimes.Idle
			deltaTotal := sumAll(curr) - sumAll(prevCPUTimes) // Calculate the total CPU delta
			prevCPUTimes = curr
			available_cpu := (deltaIdle + deltaUser) / deltaTotal * 100 // Calculate the available CPU percentage
			cpuChan <- float64(available_cpu)
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
		}(prevDropStats)

		// Wait for all goroutines to finish
		wg.Wait()
		// Close the channels
		idle_percent := <-cpuChan
		swapStat := <-memChan
		deltadrops := <-netChan
		// Append all metrics to the metrics slice
		metrics.Metrics = appendMetric(metrics.Metrics, "Idle Percent", idle_percent)
		metrics.Metrics = appendMetric(metrics.Metrics, "Memory Swaps", swapStat.swaps)
		metrics.Metrics = appendMetric(metrics.Metrics, "Memory Swaps MB", float64(swapStat.swapsMB))
		metrics.Metrics = appendMetric(metrics.Metrics, "DropsIn", float64(deltadrops.dropins))
		metrics.Metrics = appendMetric(metrics.Metrics, "DropsOut", float64(deltadrops.dropouts))
		// Set the new values for the next iteration
		DropStats.dropins = deltadrops.dropins
		DropStats.dropouts = deltadrops.dropouts
		//==========================================================================
		// Print the metrics buffer length and trim if necessary (only for debugging)
		if len(metrics.Metrics) > MetricsLen*MaxMetricBuff {
			fmt.Printf("Trimming oldest metrics...\n")
			metrics.Metrics = metrics.Metrics[MetricsLen:] // Trim the oldest metrics
		}
		//================================= Sending Data =================================

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
						sendDelta = timePast % scnInterval // Compensate for the time passed
					}
				}
				stop_time := t.Since(start)
				fmt.Printf("Asynchronous sending time: %v\n", stop_time)
			}(m)
		}
		//
		timePast += scnInterval
		stop_time := t.Since(start_time) // Calculate the time taken for the scan
		fmt.Printf("Total time taken: %v\n", stop_time)
		// Calculate the actual sleep time (to account for processing time)
		delta_sleep := (t.Duration(scnInterval) * t.Millisecond) - stop_time
		t.Sleep(delta_sleep)
	}
}
