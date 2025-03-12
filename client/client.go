package main

import (
	"context"
	"log"
	"time"

	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {

	// conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewLoadMonitorClient(conn)

	metrics := &pb.Metrics{
		Metrics: []*pb.Metric{
			{
				Id:       1,
				DataName: "cpu",
				Data:     0.8,
			},
			{
				Id:       2,
				DataName: "disk",
				Data:     0.9,
			},
			{
				Id:       3,
				DataName: "memory",
				Data:     0.6,
			},
		},
	}

	cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.LoadData(cctx, metrics)
	if err != nil {
		log.Fatalf("Error while calling LoadData: %v", err)
	}
	log.Printf("Response from server: %v", resp)

}
