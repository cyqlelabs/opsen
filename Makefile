.PHONY: all build-server build-client clean install test test-race test-coverage test-coverage-html test-short test-verbose benchmark lint deps

all: build-server build-client

build-server:
	@echo "Building load balancer server..."
	cd server && go build -o ../bin/opsen-server .

build-client:
	@echo "Building load balancer client..."
	cd client && go build -o ../bin/opsen-client .

clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/

install: all
	@echo "Installing binaries to /usr/local/bin..."
	sudo cp bin/opsen-server /usr/local/bin/
	sudo cp bin/opsen-client /usr/local/bin/
	sudo chmod +x /usr/local/bin/opsen-server
	sudo chmod +x /usr/local/bin/opsen-client
	@echo "Installing configuration files..."
	sudo mkdir -p /etc/opsen
	sudo mkdir -p /opt/opsen
	@if [ ! -f /etc/opsen/server.yml ]; then \
		sudo cp configs/server.example.yml /etc/opsen/server.yml; \
		echo "Created /etc/opsen/server.yml"; \
	else \
		echo "Skipping /etc/opsen/server.yml (already exists)"; \
	fi
	@if [ ! -f /etc/opsen/client.yml ]; then \
		sudo cp configs/client.example.yml /etc/opsen/client.yml; \
		echo "Created /etc/opsen/client.yml"; \
	else \
		echo "Skipping /etc/opsen/client.yml (already exists)"; \
	fi
	@echo "Installing systemd service files..."
	sudo cp configs/opsen-server.service /etc/systemd/system/
	sudo cp configs/opsen-client.service /etc/systemd/system/
	sudo systemctl daemon-reload
	@echo ""
	@echo "Installation complete!"
	@echo ""
	@echo "Next steps:"
	@echo "  1. Edit /etc/opsen/server.yml (server configuration)"
	@echo "  2. Edit /etc/opsen/client.yml (client configuration)"
	@echo "  3. Start services:"
	@echo "     sudo systemctl start opsen-server"
	@echo "     sudo systemctl start opsen-client"

test:
	@echo "Running tests..."
	go test -v ./...

test-race:
	@echo "Running tests with race detector..."
	go test -race ./...

test-coverage:
	@echo "Running tests with coverage..."
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

test-coverage-html:
	@echo "Generating HTML coverage report..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-short:
	@echo "Running short tests..."
	go test -short ./...

test-verbose:
	@echo "Running tests with verbose output..."
	go test -v -race ./...

benchmark:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

lint:
	@echo "Running linter..."
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy
