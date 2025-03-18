package main

import (
	"context"
	"log"
	"net"
	t "time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
)

var (
	// InfluxDB client configuration
	influxURL    = "http://localhost:8086"
	influxToken  = "dvKsoUSbn-7vW04bNFdZeL87TNissgRP43i_ttrg-Vx3LdkzKJHucylmomEasS9an7lGv_TyZRj6-dHINMjXVA=="
	influxOrg    = "vainsark"
	influxBucket = "metrics"
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
		// Define the measurement name. You can adjust this as needed.
		measurementName := "system_metrics"

		// Create a point with a tag for the metric type and a field for its value.
		p := influxdb2.NewPoint(
			measurementName,
			map[string]string{"type": metric.DataName},
			map[string]interface{}{"value": metric.Data},
			t.Now(), // Use the current time as the timestamp
		)

		// Write the point to InfluxDB
		if err := writeAPI.WritePoint(ctx, p); err != nil {
			log.Printf("Error writing point to InfluxDB: %v", err)
		}

		// Log the metric for debugging purposes
		switch metric.DataName {
		case "cpu":
			log.Printf("CPU load: %.2f%%", metric.Data)
		case "memory":
			log.Printf("Memory utilization: %.2f%%", metric.Data)
		case "disk":
			log.Printf("Disk usage: %.2f%%", metric.Data)
		default:
			log.Printf("%s: %.2f", metric.DataName, metric.Data)
		}
	}
	log.Println("=======================================")

	return &pb.MetricsAck{Ack: 1}, nil
}

func main() {

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
