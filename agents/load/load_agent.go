package main

import (
	"context"
	"fmt"
	"log"
	t "time"

	"github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	AgentID = 1
)

func updateOrAppendMetric(metrics []*pb.Metric, id int32, agentId int32, dataName string, data float64) []*pb.Metric {
	for i, m := range metrics {
		if m.DataName == dataName {
			metrics[i].Data = data
			return metrics
		}
	}
	newMetric := &pb.Metric{
		Id:       id,
		AgentId:  agentId,
		DataName: dataName,
		Data:     data,
	}
	return append(metrics, newMetric)
}

func main() {

	// conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewLoadMonitorClient(conn)

	metrics := &pb.Metrics{}
	scnInterval := 5
	timePast := 0
	//=======================================================
	type bytesstat struct {
		BytesSent uint64
		BytesRecv uint64
	}
	BytesStat := bytesstat{}
	netStats, err := net.IOCounters(false)
	if err != nil {
		log.Fatalf("Error while getting Network IO Counters: %v", err)
	}
	BytesStat.BytesSent = netStats[0].BytesSent
	BytesStat.BytesRecv = netStats[0].BytesRecv
	//=======================================================
	for {
		// MAIN LOOP
		fmt.Printf("============= Scan time: %ds ==============\n", timePast)
		// CPU Usage
		cpuPercent, err := cpu.Percent(0, false)
		if err != nil {
			log.Fatalf("Error while getting CPU usage: %v", err)
		}
		// fmt.Printf("CPU Usage: %.2f%%\n", cpuPercent)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 1, AgentID, "cpu", cpuPercent[0])

		// Memory Usage
		vmStat, err := mem.VirtualMemory()
		// fmt.Printf("Memory Usage: %.2f%% (Total: %v MB, Used: %v MB)\n", vmStat.UsedPercent, vmStat.Total/1024/1024, vmStat.Used/1024/1024)
		if err != nil {
			log.Fatalf("Error while getting memory usage: %v", err)
		}
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 2, AgentID, "memory", vmStat.UsedPercent)

		// // Swap Memory Usage
		// swapStat, err := mem.SwapMemory()
		// if err != nil {
		// 	log.Fatalf("Error while getting memory swaps: %v", err)
		// }
		// // fmt.Printf("Swap Usage: %.2f%% (Total: %v MB, Used: %v MB)\n", swapStat.UsedPercent, swapStat.Total/1024/1024, swapStat.Used/1024/1024)
		// metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 3, AgentID+2, "swaps", swapStat.UsedPercent)

		// Network Statistics
		netStats, err := net.IOCounters(false)
		if err != nil {
			log.Fatalf("Error while getting Network IO Counters: %v", err)
		}
		// for _, stat := range netStats {
		// 	fmt.Printf("Network - Sent: %v MB, Received: %v MB\n", stat.BytesSent/1024/1024, stat.BytesRecv/1024/1024)
		// }
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 3, AgentID, "BytesOut", float64(netStats[0].BytesSent-BytesStat.BytesSent)/1024)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 4, AgentID, "BytesIn", float64(netStats[0].BytesRecv-BytesStat.BytesRecv)/1024)
		fmt.Printf("Network - Sent: %v kB, Received: %v kB\n", (netStats[0].BytesSent-BytesStat.BytesSent)/1024, (netStats[0].BytesRecv-BytesStat.BytesRecv)/1024)
		BytesStat.BytesSent = netStats[0].BytesSent
		BytesStat.BytesRecv = netStats[0].BytesRecv

		// Storage (Disk Usage)
		diskStat, err := disk.Usage("/")
		if err != nil {
			log.Fatalf("Error while getting disk usage: %v", err)
		}
		// fmt.Printf("Disk Usage: %.2f%% (Total: %v GB, Used: %v GB)\n", diskStat.UsedPercent, diskStat.Total/1024/1024/1024, diskStat.Used/1024/1024/1024)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 5, AgentID, "disk", diskStat.UsedPercent)

		//=======================================================

		ctx, cancel := context.WithTimeout(context.Background(), 5*t.Second)
		resp, err := client.LoadData(ctx, metrics)
		cancel() // cancel the context after the call finishes

		if err != nil {
			log.Printf("Error while calling LoadData: %v", err)
		} else {
			log.Printf("Response from server: %v", resp)
		}

		timePast += scnInterval
		t.Sleep(t.Duration(scnInterval) * t.Second)

	}
}
