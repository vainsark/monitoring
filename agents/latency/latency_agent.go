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
	"sync"
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
	arraySizeBytes = 1024 * 1024 // 1MB of memory
)

var (
	scnInterval   = 2000 //scan interval in milliseconds
	TransmitMult  = 1    // multiplier for the scan interval
	MaxMetricBuff = 10   // max number of metrics to keep in the buffer
	timePast      = 0
	sendDelta     = 0
	MetricsLen    = 9 // number of metrics per iteration
	ServerIP      = "localhost"
	DiskName      = ids.StorageLinuxID // name of the disk to monitor
	devID         = ids.DeviceId
	agentId       = ids.LatencyID
)

type DiskResult struct {
	r_wait float64
	w_wait float64
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

	deviation := rand.NormFloat64() * 50
	if deviation < 0 {
		deviation = -deviation
	}
	devTime := t.Duration(deviation) * t.Microsecond
	fmt.Println("Sensoric FUNCTION")
	return minRT + devTime
}
func timetosend() bool {
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
	// Set the device ID and agent ID
	metrics := &pb.Metrics{DeviceId: devID, AgentId: agentId}

	for {
		// MAIN LOOP
		fmt.Printf("============= Latency Scan time: %ds ==============\n", timePast/1000)
		start_time := t.Now()

		var wg sync.WaitGroup
		wg.Add(4) // Number of goroutines to wait for
		// cpuChan := make(chan float64, 1)
		memChan := make(chan t.Duration, 1)
		diskchan := make(chan DiskResult, 1)
		netChan := make(chan float64, 1)
		sensorChan := make(chan t.Duration, 1)
		//============================= Memory Latency =============================
		go func() {
			defer wg.Done()
			latency := measureMemoryLatency()
			memChan <- latency
		}()
		//============================= Storage (IOSTAT) =============================
		go func() {
			defer wg.Done()
			cmd := exec.Command("bash", "-c", "iostat -dx "+DiskName+" 1 2| awk 'NR>2 {print $6, $12}'")
			output, err := cmd.Output()
			if err != nil {
				log.Fatalf("Error running iostat command: %v", err)
			}
			outputStr := string(output)
			fields := strings.Fields(outputStr)
			r_wait, _ := strconv.ParseFloat(fields[6], 64)
			w_wait, _ := strconv.ParseFloat(fields[7], 64)
			diskchan <- DiskResult{r_wait: r_wait, w_wait: w_wait}
		}()
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
			pinger.Count = 1
			pinger.SetPrivileged(true)
			err = pinger.Run()
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
		latency := <-memChan
		diskResult := <-diskchan
		AvgRtt := <-netChan
		senseRT := <-sensorChan
		//=======================================================
		metrics.Metrics = appendMetric(metrics.Metrics, "Memory Latency", float64(latency))
		metrics.Metrics = appendMetric(metrics.Metrics, "read wait", diskResult.r_wait)
		metrics.Metrics = appendMetric(metrics.Metrics, "write wait", diskResult.w_wait)
		metrics.Metrics = appendMetric(metrics.Metrics, "NetInterLaten", AvgRtt)
		metrics.Metrics = appendMetric(metrics.Metrics, "Sensor Latency", float64(senseRT)/1000)
		//==========================================================================
		// Print the metrics buffer length and trim if necessary
		fmt.Printf("metrics length: %v\n", len(metrics.Metrics))
		if len(metrics.Metrics) > MetricsLen*MaxMetricBuff {
			fmt.Printf("Trimming oldest metrics...\n")
			metrics.Metrics = metrics.Metrics[MetricsLen:] // Trim the oldest metrics
		}
		//================================= Sending Data =================================
		if timetosend() {
			// Save current batch in a local variable.
			m := metrics
			metrics = &pb.Metrics{DeviceId: devID, AgentId: agentId} // new metrics.
			// Send the data to the server
			go func(m *pb.Metrics) {
				start := t.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 5*t.Second)
				resp, err := client.LoadData(ctx, m)
				cancel()
				if err != nil {
					log.Printf("Error while asynchronously sending data: %v", err)
				} else {
					log.Printf("Asynchronous response: %v", resp)
					TransmitMult = int(resp.TransMult)
					newScnInterval := int(resp.ScnFreq)
					if scnInterval != newScnInterval {
						scnInterval = newScnInterval
						sendDelta = timePast % scnInterval
					}
				}
				stop_time := t.Since(start)
				fmt.Printf("Asynchronous sending time: %v\n", stop_time)
			}(m)
		}

		timePast += scnInterval
		stop_time := t.Since(start_time)
		fmt.Printf("Total time taken: %v\n", stop_time)
		delta_sleep := (t.Duration(scnInterval) * t.Millisecond) - stop_time
		t.Sleep(delta_sleep)
	}
}
