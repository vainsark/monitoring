package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	t "time"

	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	ServerIP = "localhost"
)

func main() {
	fmt.Println(os.Args)
	Params := &pb.UserParams{}
	// Set the server IP address from command line argument or use default
	if len(os.Args) > 1 {
		arg := 1
		if len(os.Args) < 4 {
			ServerIP = os.Args[arg]
		} else {
			arg = 0
		}
		Params.DeviceId = os.Args[arg+1]
		freq, _ := strconv.Atoi(os.Args[arg+2])
		mult, _ := strconv.Atoi(os.Args[arg+3])
		Params.ScnFreq = int32(freq)
		Params.TransMult = int32(mult)
	}
	log.Printf("Server IP: %s:50051\n", ServerIP)
	log.Printf("Device ID: \"%s\"\n		    Scan Frequency: %dms\n		    Transmit Multiplier: %d\n", Params.DeviceId, Params.ScnFreq, Params.TransMult)

	// Connect to the server
	// Initialize the gRPC connection
	conn, err := grpc.NewClient(ServerIP+":50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer conn.Close()

	// Create a new gRPC client
	client := pb.NewUserInputClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*t.Second)
	resp, err := client.ScanParams(ctx, Params)
	cancel()
	if err != nil {
		log.Fatalf("Error while calling: %v", err)
	}
	log.Printf("Response from server: %v", resp.Ack)

}
