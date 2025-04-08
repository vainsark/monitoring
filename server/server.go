package main

import (
	"context"
	"log"
	"net"
	"os"
	"strconv"
	t "time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
)

var (
	// InfluxDB client configuration
	influxURL   = "http://localhost:8086"
	influxToken = "dvKsoUSbn-7vW04bNFdZeL87TNissgRP43i_ttrg-Vx3LdkzKJHucylmomEasS9an7lGv_TyZRj6-dHINMjXVA=="
	// influxToken  = "Ib2fq58MyBy2OUR9Aa3Lv2BN1uNBYnwMTsx4pyOSDzqoLZF6qKMTnfsB7hRO0_aFwxEOUPtbt3NUmyvs8RyhCw==" // Laptop
	influxOrg          = "vainsark"
	influxBucket       = "metrics"
	scan         int32 = 5 // Default scan interval in seconds
	transmit     int32 = 1 // Default transmit interval multiplier
)

type server struct {
	pb.UnimplementedLoadMonitorServer
	influxClient influxdb2.Client
	org          string
	bucket       string
}

func (s *server) LoadData(ctx context.Context, in *pb.Metrics) (*pb.MetricsAck, error) {
	// Create a blocking write API instance.
	writeAPI := s.influxClient.WriteAPIBlocking(s.org, s.bucket)

	for _, metric := range in.Metrics {
		// Define the Agent name.
		agentName := ""
		switch metric.AgentId {
		case 1:
			agentName = "Load"
		case 2:
			agentName = "Latency"
		case 3:
			agentName = "Availability"
		default:
			agentName = "Misc"
		}

		// Use the metric's timestamp and adjust for time zone. If not provided, use the current time.
		pointTime := t.Now()
		if metric.Timestamp != nil {
			pointTime = metric.Timestamp.AsTime().Local()
		}

		// Create an entry the agent name and a field for its value.
		entry := influxdb2.NewPoint(
			agentName,
			map[string]string{"type": metric.DataName},
			map[string]interface{}{"value": metric.Data},
			pointTime,
		)

		// Write the point to InfluxDB
		if err := writeAPI.WritePoint(ctx, entry); err != nil {
			log.Printf("Error writing point to InfluxDB: %v", err)
		}

		// Log the metric for debugging purposes
		// log.Printf("%s (%s): %.2f	(time: %s)", metric.DataName, agentName, metric.Data, metric.Timestamp.AsTime().Local().Format("2006-01-02 15:04:05"))
		log.Printf("%s (%s): %.2f", metric.DataName, agentName, metric.Data)

	}
	log.Println("=======================================")

	return &pb.MetricsAck{Ack: 1, ScnFreq: scan, TransMult: transmit}, nil
}

func main() {
	// Check if there are arguments for scan and transmit intervals and get them.
	if len(os.Args) >= 3 {
		scanArg, _ := strconv.Atoi(os.Args[1])
		transmitArg, _ := strconv.Atoi(os.Args[2])
		scan = int32(scanArg)
		transmit = int32(transmitArg)
	}
	log.Printf("Scan frequency: %d seconds", scan)
	log.Printf("Transmit multiplier: %d", transmit)

	// Initialize the InfluxDB client.
	client := influxdb2.NewClient(influxURL, influxToken)
	defer client.Close()

	// Create a new server instance with InfluxDB references.
	s := &server{
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

	log.Printf("Server listening at %v", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
