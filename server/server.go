package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	t "time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/vainsark/monitoring/agents/ids"
	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
)

const (
	// InfluxDB client configuration
	influxURL     = ids.InfluxURL
	influxToken   = ids.InfluxToken
	influxOrg     = ids.InfluxOrg
	influxBucket  = ids.InfluxBucket
	MonitorConfig = "device_params"
)

var (
	scan            int32 = 3000 // Default scan interval in seconds
	transmit        int32 = 1    // Default transmit interval multiplier
	deviceParamsMap       = make(map[string]map[int32]agentParams)
)

type agentParams struct {
	ScnFreq   int32
	TransMult int32
}

type server struct {
	// Embed the unimplemented server
	pb.UnimplementedLoadMonitorServer
	influxClient influxdb2.Client
	org          string
	bucket       string
}

type userService struct {
	// Embed the unimplemented server
	pb.UnimplementedUserInputServer
	influxClient influxdb2.Client
	org          string
	bucket       string
}

func getAgentName(agentID int32) string {
	// Map the agent id (int32) to a readable name (string).
	switch agentID {
	case 1:
		return "Load"
	case 2:
		return "Latency"
	case 3:
		return "Availability"
	default:
		return "Misc"
	}
}
func getAgentID(agentID string) int32 {
	// Map the agent name (string) to an id (int32).
	switch agentID {
	case "Load":
		return 1
	case "Latency":
		return 2
	case "Availability":
		return 3
	default:
		return 0
	}
}

func storeParams(deviceID string, agentID int32, p agentParams, w api.WriteAPIBlocking, ctx context.Context) error {
	/* Store the parameters in InfluxDB. */
	entry := influxdb2.NewPoint(
		MonitorConfig,
		map[string]string{
			"deviceId": deviceID,
			"agentId":  getAgentName(agentID),
		},
		map[string]interface{}{
			"ScnFreq":   p.ScnFreq,
			"TransMult": p.TransMult,
		},
		// 0 timestamp to update the entry instead of creating a new one
		t.Unix(0, 0),
	)
	return w.WritePoint(ctx, entry)
}

func loadParams(client influxdb2.Client) error {
	/* Load the parameters from InfluxDB. */

	flux := fmt.Sprintf(`
		from(bucket:"%s")
		|> range(start:0)
		|> filter(fn: (r) => r._measurement == "%s" 
		and (r._field == "ScnFreq" or r._field == "TransMult"))`,
		influxBucket, MonitorConfig)
	// Create a query API instance
	queryAPI := client.QueryAPI(influxOrg)
	result, err := queryAPI.Query(context.Background(), flux)
	if err != nil {
		return err
	}
	defer result.Close()

	temp := make(map[string]map[int32]agentParams)
	// Iterate over query response
	for result.Next() {
		// Parse the current result from the query
		rec := result.Record()
		devID := rec.ValueByKey("deviceId").(string)
		agentName := rec.ValueByKey("agentId").(string) // now a string tag
		agentId := getAgentID(agentName)                // convert to int32
		field := rec.Field()                            // "ScnFreq" or "TransMult"
		val := int32(rec.Value().(int64))               // the numeric value

		// Make sure the inner map exists
		if _, ok := temp[devID]; !ok {
			temp[devID] = make(map[int32]agentParams)
		}
		// Pull out the in‑progress struct (zero‑value if first time)
		ap := temp[devID][agentId]
		// Fill in whichever field this row represents
		switch field {
		case "ScnFreq":
			ap.ScnFreq = int32(val)
		case "TransMult":
			ap.TransMult = int32(val)
		}

		// Write it back into the temp map
		temp[devID][agentId] = ap
	}
	if result.Err() != nil {
		return result.Err()
	}
	// Now that each agentParams is fully populated, call setParams:
	for devID, agents := range temp {
		for agentName, ap := range agents {
			setParams(devID, agentName, ap)
		}
	}

	return nil
}

func setParams(deviceID string, agentID int32, p agentParams) {
	/*Check for device's params map
	If it doesn't exist, create a new map for the device */
	if _, exists := deviceParamsMap[deviceID]; !exists {
		log.Printf("Creating new params map for Device: %s\n", deviceID)
		deviceParamsMap[deviceID] = make(map[int32]agentParams)
	}
	// write into the inner map
	log.Printf("Setting params for Device: %s, Agent: %d\n", deviceID, agentID)
	deviceParamsMap[deviceID][agentID] = p
}
func getParams(deviceID string, agentID int32) (agentParams, bool) {
	// check the outer map
	agents, ok := deviceParamsMap[deviceID]
	if !ok {
		return agentParams{}, false
	}
	// then check the inner map
	p, found := agents[agentID]
	return p, found
}

func (s *server) LoadData(ctx context.Context, in *pb.Metrics) (*pb.MetricsAck, error) {
	/* Receive the metrics from the agent and store them in InfluxDB. */

	// Create a blocking write API instance.
	writeAPI := s.influxClient.WriteAPIBlocking(s.org, s.bucket)

	// Get the device ID from the metric.
	deviceID := in.DeviceId
	log.Printf("Received Load Data for Device: %s\n", deviceID)
	// Define the Agent name.

	agentName := getAgentName(in.AgentId)

	log.Printf("Agent: %s\n", agentName)
	for _, metric := range in.Metrics {
		// Use the metric's timestamp and adjust for time zone. If not provided, use the current time.
		pointTime := t.Now()
		if metric.Timestamp != nil {
			pointTime = metric.Timestamp.AsTime().Local()
		}
		// Create an entry the agent name and a field for its value.
		entry := influxdb2.NewPoint(
			agentName,
			map[string]string{
				"type":     metric.DataName,
				"deviceId": deviceID},
			map[string]interface{}{"value": metric.Data},
			pointTime,
		)

		// Write the point to InfluxDB
		if err := writeAPI.WritePoint(ctx, entry); err != nil {
			log.Printf("Error writing point to InfluxDB: %v", err)
		}
		// Print the metric name and value to the console.
		log.Printf("%s (%s): %.2f", metric.DataName, agentName, metric.Data)

	}
	log.Println("=======================================")
	if params, exists := getParams(deviceID, in.AgentId); exists {
		// Use the device parameters
		return &pb.MetricsAck{Ack: 1, ScnFreq: params.ScnFreq, TransMult: params.TransMult}, nil

	} else {
		setParams(deviceID, in.AgentId, agentParams{ScnFreq: scan, TransMult: transmit})
		return &pb.MetricsAck{Ack: 1, ScnFreq: scan, TransMult: transmit}, nil
	}

}
func (s *userService) ScanParams(ctx context.Context, in *pb.UserParams) (*pb.UserParamsAck, error) {
	/* Receive the scan period and transmit multiplier for certaion agent from the user and store them in InfluxDB. */
	writeAPI := s.influxClient.WriteAPIBlocking(s.org, s.bucket)

	// Update the scan and transmit intervals based on user input.
	agentName := getAgentName(in.AgentId)
	log.Printf("Received Scan Params for Device: %s", in.DeviceId)
	log.Printf("Received Agent ID: %s", agentName)
	log.Printf("Updated scan frequency: %d miliseconds", in.ScnFreq)
	log.Printf("Updated transmit multiplier: %d", in.TransMult)
	params := agentParams{
		ScnFreq:   in.ScnFreq,
		TransMult: in.TransMult,
	}

	setParams(in.DeviceId, in.AgentId, params) // Update the in-memory map
	// Store the parameters in InfluxDB.
	err := storeParams(in.DeviceId, in.AgentId, params, writeAPI, ctx)
	if err != nil {
		log.Printf("Error writing params to InfluxDB: %v", err)
		return &pb.UserParamsAck{Ack: 0}, nil
	}
	return &pb.UserParamsAck{Ack: 1}, nil

}

func main() {
	// Check if there are arguments for scan and transmit intervals and get them.
	if len(os.Args) >= 3 {
		scanArg, _ := strconv.Atoi(os.Args[1])
		transmitArg, _ := strconv.Atoi(os.Args[2])
		scan = int32(scanArg)
		transmit = int32(transmitArg)
	}
	log.Printf("Scan frequency: %d miliseconds\n", scan)
	log.Printf("Transmit multiplier: %d\n", transmit)

	// Initialize the InfluxDB client.
	client := influxdb2.NewClient(influxURL, influxToken)
	defer client.Close()

	// preload any existing params into memory
	if err := loadParams(client); err != nil {
		log.Fatalf("failed to load params from InfluxDB: %v", err)
	}

	// Create a new server instance
	s := &server{
		influxClient: client,
		org:          influxOrg,
		bucket:       influxBucket,
	}
	// Create a user service instance (for updating monitoring params)
	us := &userService{
		influxClient: client,
		org:          influxOrg,
		bucket:       influxBucket,
	}

	// Listen on TCP port 50051.
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	// Create and start the gRPC server.
	grpcServer := grpc.NewServer()
	pb.RegisterLoadMonitorServer(grpcServer, s)

	// Register the user service with the gRPC server.
	pb.RegisterUserInputServer(grpcServer, us)

	log.Printf("Server listening at %v\n", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
