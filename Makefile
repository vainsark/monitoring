
# =====================
# export PATH=$PATH:$(go env GOPATH)/bin

# sudo /usr/local/go/bin/go run latency_agent.go 

# go run agents/latency/latency_agent.go

.PHONY: docker-up run-all server-run load-run latency-run availability-run stress_cpu stress_disk stress_mem stress_mem_lat

docker-up:
	cd docker && docker-compose up -d

server-run:
	go run server/server.go $(scan) $(mult)

load-run:
	go run agents/load/load_agent.go $(server)

latency-run:
	sudo /usr/local/go/bin/go run agents/latency/latency_agent.go $(server)

availability-run:
	go run agents/availability/availability_agent.go $(server)
	
run-all:
	go run agents/load/load_agent.go $(server) & \
	sudo /usr/local/go/bin/go run agents/latency/latency_agent.go $(server) & \
	go run agents/availability/availability_agent.go $(server) & \

sensor-sim-run:
	go run sensoric_sim/TrafficSim.go 
	
generate_grpc_code:
	protoc \
		--go_out=loadmonitor_proto \
		--go_opt=paths=source_relative \
		--go-grpc_out=loadmonitor_proto \
		--go-grpc_opt=paths=source_relative \
		loadmonitor.proto


stress_cpu:
	stress --cpu 1 --timeout 30s
stress_mem:
	stress --vm 1 --vm-bytes 512M --timeout 30s
stress_mem_lat:
	stress --vm 20 --vm-bytes 32M --timeout 30s
stress_disk:
	stress --hdd 2 --timeout 30s
