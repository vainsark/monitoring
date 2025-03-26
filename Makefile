
# =====================
# export PATH=$PATH:$(go env GOPATH)/bin

# sudo /usr/local/go/bin/go run latency_agent.go 

# go run agents/latency/latency_agent.go

.PHONY: docker-up server-run load-run latency-run availability-run

docker-up:
	cd docker && docker-compose up -d

server-run:
	go run server/server.go

load-run:
	go run agents/load/load_agent.go

latency-run:
	sudo /usr/local/go/bin/go run agents/latency/latency_agent.go

availability-run:
	go run agents/availability/availability_agent.go
	
	
generate_grpc_code:
	protoc \
		--go_out=loadmonitor_proto \
		--go_opt=paths=source_relative \
		--go-grpc_out=loadmonitor_proto \
		--go-grpc_opt=paths=source_relative \
		loadmonitor.proto


