package main

import (
	"context"
	"log"
	"net"

	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
	// influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

var (
	// InfluxDB client configuration
	influxURL = "http://localhost:8086"
)

type server struct {
	pb.UnimplementedLoadMonitorServer
}

func (s *server) LoadData(ctx context.Context, in *pb.Metrics) (*pb.MetricsAck, error) {
	for _, metric := range in.Metrics {
		switch metric.DataName {
		case "cpu":
			log.Printf("CPU load: %.2f%%", metric.Data)
		case "memory":
			log.Printf("Memory utilization: %.2f%%", metric.Data)
		case "disk":
			log.Printf("Disk usage: %.2f%%", metric.Data)
		default:
			log.Printf("%s: %f", metric.DataName, metric.Data)
		}
	}
	log.Println("=======================================")

	return &pb.MetricsAck{Ack: 1}, nil
}

func main() {

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()

	pb.RegisterLoadMonitorServer(grpcServer, &server{})

	log.Printf("Server listening at %v", lis.Addr())

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
