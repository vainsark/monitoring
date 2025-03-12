package main

import (
	"context"
	"log"
	"net"

	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedLoadMonitorServer
}

func (s *server) LoadData(ctx context.Context, in *pb.Metrics) (*pb.MetricsAck, error) {
	log.Printf("Received metrics: %v", in)

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
