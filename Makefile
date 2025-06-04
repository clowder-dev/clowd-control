# Makefile for the ClowdControl project

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMODTIDY=$(GOCMD) mod tidy

# Binary name
BINARY_NAME=clowd-control
# Main package location
MAIN_PACKAGE=./cmd/cli/main.go

.PHONY: all build test clean deps

all: build

# Build the application binary
build: deps
	@echo "Building $(BINARY_NAME)..."
	$(GOBUILD) -o $(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "$(BINARY_NAME) built successfully."

# Run all tests
test: deps
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Clean up build artifacts
clean:
	@echo "Cleaning up..."
	$(GOCMD) clean
	rm -f $(BINARY_NAME)
	@echo "Cleanup complete."

# Ensure dependencies are up to date
deps:
	@echo "Ensuring dependencies are up to date..."
	$(GOMODTIDY)

.PHONY: run
# Run the application
# Allows passing arguments to the binary via ARGS, e.g., make run ARGS="-v --config myconfig.yaml"
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME) $(ARGS)
