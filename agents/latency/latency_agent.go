package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"sync"
	t "time"

	"github.com/go-ping/ping"
	"github.com/shirou/gopsutil/disk"
	"github.com/vainsark/monitoring/agents/ids"
	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	numAccesses    = 1000000
	arraySizeBytes = 1024 * 1024 // 1MB of memory
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
	agentId       = ids.LatencyID
)

type DiskResult struct {
	r_wait float64
	w_wait float64
}
type prevDiskStat struct {
	ReadCount  uint64
	ReadTime   uint64
	WriteCount uint64
	WriteTime  uint64
}

func getDefIP() string {
	/*	Returns the default IP address of the system. */
	cmd := exec.Command("bash", "-c", "ip route | grep default")
	output, err := cmd.Output()
	if err != nil {
		log.Fatalf("Error running command: %v", err)
	}
	fields := strings.Fields(string(output))
	defaultIP := fields[2]
	fmt.Println("Default IP:", defaultIP)
	return defaultIP
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

func MeasureCPUAvg() (float64, error) {
	cmdLine := "sudo cyclictest -t 0 -i 1000 -p 99 -l 100 -q"
	cmd := exec.Command("bash", "-c", cmdLine)

	// run and capture everything
	out, err := cmd.CombinedOutput()
	if err != nil {
		// log the full output for debugging
		log.Printf("cyclictest failed: %v\noutput:\n%s", err, out)
		return 0, fmt.Errorf("cyclictest exit: %w", err)
	}

	var sum, count int
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "T:") {
			// skip lines that are not results"
			continue
		}
		var (
			thread, pid, pri, interval, loops, min, act, avg, max int
		)
		// pull out all values from the result line
		if _, err := fmt.Sscanf(
			line,
			"T: %d (%d) P:%d I:%d C:%d Min:%d Act:%d Avg:%d Max:%d",
			&thread, &pid, &pri, &interval, &loops,
			&min, &act, &avg, &max,
		); err != nil {
			log.Printf("failed to parse line %q: %v", line, err)
			continue
		}
		sum += avg
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scanner error: %w", err)
	}
	if count == 0 {
		return 0, fmt.Errorf("no data lines parsed from cyclictest output")
	}
	return float64(sum) / float64(count), nil
}

func measureMemoryLatency() t.Duration {
	/*	Simulates multiple random access to an array and measures the time taken for each access.
		Returns the average latency.*/

	numElements := arraySizeBytes / 4
	buffer := make([]int32, numElements)
	// Initialize buffer
	for i := 0; i < numElements; i++ {
		buffer[i] = int32(i)
	}
	var total t.Duration
	for i := 0; i < numAccesses; i++ {
		index := rand.Intn(numElements)
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
	/*	simulate reading 64bit from sensor register with I2C at 400 kHz	*/
	minRT := 250 * t.Microsecond

	deviation := rand.NormFloat64() * 30
	if deviation < 0 {
		deviation = -deviation
	}
	devTime := t.Duration(deviation) * t.Microsecond
	fmt.Println("Sensoric FUNCTION")
	return minRT + devTime
}
func timetosend() bool {
	/*	/ Calculate the time since the last send and corrects for correct modulus */
	return timePast == 0 || ((timePast-sendDelta)%(scnInterval*TransmitMult) == 0)
}
func main() {
	// Getting the default IP address
	defaultIP := getDefIP()
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

	metrics := &pb.Metrics{DeviceId: devID, AgentId: agentId}
	// Set the device ID and agent ID
	initial, err := disk.IOCounters(DiskName)
	if err != nil {
		log.Fatalf("Error getting initial disk IO counters: %v", err)
	}
	DiskStats := prevDiskStat{
		ReadCount:  initial[DiskName].ReadCount,
		ReadTime:   initial[DiskName].ReadTime,
		WriteCount: initial[DiskName].WriteCount,
		WriteTime:  initial[DiskName].WriteTime,
	}
	for {
		// MAIN LOOP
		fmt.Printf("============= Latency Scan time: %ds ==============\n", timePast/1000)
		start_time := t.Now() // Start the timer for the scan

		var wg sync.WaitGroup
		wg.Add(5) // Number of goroutines to wait for
		// Create channels for each goroutine
		cpuChan := make(chan float64, 1)
		memChan := make(chan t.Duration, 1)
		diskchan := make(chan DiskResult, 1)
		netChan := make(chan float64, 1)
		sensorChan := make(chan t.Duration, 1)
		//============================= CPU Latency =============================
		go func() {
			defer wg.Done()
			avg, err := MeasureCPUAvg()
			if err != nil {
				log.Printf("cyclictest error: %v", err)
				cpuChan <- 0
				return
			}
			fmt.Printf("Cyclictest average: %.2f\n", avg)
			cpuChan <- avg
		}()
		//============================= Memory Latency =============================
		go func() {
			defer wg.Done()
			latency := measureMemoryLatency()
			memChan <- latency
		}()
		//============================= Storage Latency =============================
		prevDiskStats := DiskStats // Save the previous disk stats
		go func(prev prevDiskStat) {
			defer wg.Done()

			stats, err := disk.IOCounters(DiskName)
			if err != nil {
				log.Printf("Error getting disk IO counters: %v", err)
				diskchan <- DiskResult{}
				return
			}
			curr := stats[DiskName]

			// compute deltas
			readCountDelta := curr.ReadCount - prevDiskStats.ReadCount
			readTimeDelta := curr.ReadTime - prevDiskStats.ReadTime
			writeCountDelta := curr.WriteCount - prevDiskStats.WriteCount
			writeTimeDelta := curr.WriteTime - prevDiskStats.WriteTime

			// compute waits
			var rAwait, wAwait float64
			if readCountDelta > 0 {
				rAwait = float64(readTimeDelta) / float64(readCountDelta)
			}
			if writeCountDelta > 0 {
				wAwait = float64(writeTimeDelta) / float64(writeCountDelta)
			}
			DiskStats.ReadCount = stats[DiskName].ReadCount
			DiskStats.ReadTime = stats[DiskName].ReadTime
			DiskStats.WriteCount = stats[DiskName].WriteCount
			DiskStats.WriteTime = stats[DiskName].WriteTime

			diskchan <- DiskResult{r_wait: rAwait, w_wait: wAwait}
		}(prevDiskStats)
		//============================= Network Statistics =============================
		go func() {
			defer wg.Done()
			// Run the pinger
			pinger, err := ping.NewPinger(defaultIP)
			if err != nil {
				log.Printf("Failed to initialize pinger: %v", err)
				netChan <- 0
				return
			}
			pinger.Count = 1 // Send only one ping
			pinger.SetPrivileged(true)
			err = pinger.Run()                                               // Run the pinger
			timeout := t.Duration(float64(scnInterval)*0.75) * t.Millisecond // Set the timeout to 75% of the scan interval
			pinger.Timeout = timeout                                         // Set the timeout for the pinger
			if err != nil {
				log.Printf("Ping failed: %v", err)
				netChan <- 0
				return
			}
			// Get the results of the pings
			stats := pinger.Statistics()
			AvgRtt := float64(t.Duration(stats.AvgRtt).Microseconds()) / 1000 // Convert to milliseconds
			netChan <- AvgRtt
		}()
		//============================= Sensoric Latency =============================
		go func() {
			defer wg.Done()
			senseRT := simulSenseLatency()
			sensorChan <- senseRT
		}()
		//=======================================================
		// Wait for all goroutines to finish
		wg.Wait()
		// Close the channels
		cpuAvg := <-cpuChan
		latency := <-memChan
		diskResult := <-diskchan
		AvgRtt := <-netChan
		senseRT := <-sensorChan
		// Append all metrics to the metrics slice
		metrics.Metrics = appendMetric(metrics.Metrics, "CPU Latency", cpuAvg)
		metrics.Metrics = appendMetric(metrics.Metrics, "Memory Latency", float64(latency))
		metrics.Metrics = appendMetric(metrics.Metrics, "NetInterLaten", AvgRtt)
		metrics.Metrics = appendMetric(metrics.Metrics, "read wait", diskResult.r_wait)
		metrics.Metrics = appendMetric(metrics.Metrics, "write wait", diskResult.w_wait)
		metrics.Metrics = appendMetric(metrics.Metrics, "Sensor Latency", float64(senseRT)/1000)

		//==========================================================================
		// Print the metrics buffer length and trim if necessary (only for debugging)
		fmt.Printf("metrics length: %v\n", len(metrics.Metrics))
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
			// Send the data to the server
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

		timePast += scnInterval
		stop_time := t.Since(start_time) // Calculate the time taken for the scan
		fmt.Printf("Total time taken: %v\n", stop_time)
		// Calculate the actual sleep time (to account for processing time)
		delta_sleep := (t.Duration(scnInterval) * t.Millisecond) - stop_time
		t.Sleep(delta_sleep)
	}
}
