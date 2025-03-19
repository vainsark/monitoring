# # Set your Grafana URL and API key (ensure these are secured appropriately)
# GRAFANA_URL=http://localhost:3000
# GRAFANA_API_KEY=YOUR_GRAFANA_API_KEY
# DASHBOARD_DIR=/monitoring/grafana

# # Target to export dashboards from Grafana to JSON files
# export-dashboards:
# 	@echo "Exporting dashboards..."
# 	# Example curl command to get a dashboard by UID (repeat or script for multiple)
# 	curl -s -H "Authorization: Bearer $(GRAFANA_API_KEY)" "$(GRAFANA_URL)/api/dashboards/uid/your-dashboard-uid" -o $(DASHBOARD_DIR)/your-dashboard.json

# # Target to import dashboards from JSON files into Grafana
# import-dashboards:
# 	@echo "Importing dashboards..."
# 	# Example curl command to post a dashboard (repeat or script for multiple)
# 	curl -s -X POST -H "Content-Type: application/json" -H "Authorization: Bearer $(GRAFANA_API_KEY)" \
# 	  -d @$DASHBOARD_DIR/your-dashboard.json \
# 	  $(GRAFANA_URL)/api/dashboards/db

# =====================
# export PATH=$PATH:$(go env GOPATH)/bin

# sudo /usr/local/go/bin/go run latency_agent.go 

generate_grpc_code:
	protoc \
		--go_out=loadmonitor_proto \
		--go_opt=paths=source_relative \
		--go-grpc_out=loadmonitor_proto \
		--go-grpc_opt=paths=source_relative \
		loadmonitor.proto


