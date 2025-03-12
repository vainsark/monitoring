generate_grpc_code:
	protoc \
		--go_out=loadmonitor_proto \
		--go_opt=paths=source_relative \
		--go-grpc_out=loadmonitor_proto \
		--go-grpc_opt=paths=source_relative \
		loadmonitor.proto
