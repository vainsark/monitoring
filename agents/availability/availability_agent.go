package main

import (
	"context"
	"fmt"
	"log"
	t "time"

	"github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/v4/mem"
	pb "github.com/vainsark/monitoring/loadmonitor_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	AgentID = 3
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
	type dropstats struct {
		dropins  uint64
		dropouts uint64
	}
	DropStats := dropstats{}
	netStats, err := net.IOCounters(false)
	if err != nil {
		log.Fatalf("Error while getting Network IO Counters: %v", err)
	}
	DropStats.dropins = netStats[0].Dropin
	DropStats.dropouts = netStats[0].Dropout
	//=======================================================

	for {
		fmt.Printf("============= Scan time: %ds ==============\n", timePast)
		// // CPU Usage
		// cputime, err := cpu.Times(false)
		// if err != nil {
		// 	log.Fatalf("Error while getting CPU usage: %v", err)
		// }
		// metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 1, AgentID, "cpu", cpuPercent[0])

		// Memory Swaps
		swapStat, err := mem.SwapMemory()
		if err != nil {
			log.Fatalf("Error while getting memory swaps: %v", err)
		}
		// fmt.Printf("Swap Usage: %.2f%% (Total: %v MB, Used: %v MB)\n", swapStat.UsedPercent, swapStat.Total/1024/1024, swapStat.Used/1024/1024)
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 1, AgentID, "Memory Swaps", swapStat.UsedPercent)

		// Network Statistics
		netStats, err := net.IOCounters(false)
		if err != nil {
			log.Fatalf("Error while getting network IO counters: %v", err)
		}
		deltain := netStats[0].Dropin - DropStats.dropins
		deltaout := netStats[0].Dropout - DropStats.dropouts
		DropStats.dropins = netStats[0].Dropin
		DropStats.dropouts = netStats[0].Dropout
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 2, AgentID, "DropsIn", float64(deltain))
		metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 3, AgentID, "DropsOut", float64(deltaout))

		fmt.Printf("Dropped In: %v Packets, Dropped Out: %v Packets\n", deltain, deltaout)

		// // Storage (Disk Usage)
		// diskStat, err := disk.Usage("/")
		// if err != nil {
		// 	log.Fatalf("Error while getting disk usage: %v", err)
		// }
		// fmt.Printf("Disk Usage: %.2f%% (Total: %v GB, Used: %v GB)\n", diskStat.UsedPercent, diskStat.Total/1024/1024/1024, diskStat.Used/1024/1024/1024)
		// metrics.Metrics = updateOrAppendMetric(metrics.Metrics, 4, AgentID, "disk", diskStat.UsedPercent)

		//=======================================================

		ctx, cancel := context.WithTimeout(context.Background(), 5*t.Second)
		resp, err := client.LoadData(ctx, metrics)
		cancel()

		if err != nil {
			log.Printf("Error while calling LoadData: %v", err)
		} else {
			log.Printf("Response from server: %v", resp)
		}

		timePast += scnInterval
		t.Sleep(t.Duration(scnInterval) * t.Second)

	}
}
