.PHONY: release-all build-custom-provisioner image-custom-provisioner push-custom-provisioner

# Colors for output
WARNC = \033[0;33m
NC = \033[0m # No color

# Variables
PWD := $(shell pwd)
USER := $(shell id -u)
GROUP := $(shell id -g)
BRANCH := $(shell git rev-parse --abbrev-ref HEAD | sed s/\\//-/g)
HASH := $(shell git rev-parse --short HEAD)
BUILDTIME := $(shell date +%F-%H%I%S)
ROOTDIR := $(PWD)
stage := 1
GO_VERSION = 1.19
LDFLAGS := -ldflags "-X main.Version=${VERSION} -w -extldflags -static"

# Define tag for docker image
ifeq ($(stage), 1)
	tag := $(BRANCH)_$(HASH)_$(BUILDTIME)
endif

# Docker image name
IMG ?= siming.net/sre/custom-provisioner:$(tag)

# Go binary output folder
BIN_DIR := ./bin

# Main application source path
MAIN_SRC := ./cmd/main.go

# Build the custom provisioner binary from source code
build-custom-provisioner:
	@echo "$(WARNC)Building custom provisioner binary file with tag $(tag)...$(NC)"
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/custom-provisioner $(MAIN_SRC)

# Build docker image from the binary file
image-custom-provisioner:
	@echo "$(WARNC)Building custom provisioner Docker image with tag $(tag)...$(NC)"
	docker build -f ./deploy/docker/Dockerfile -t $(IMG) .

# Push docker image to the registry
push-custom-provisioner:
	@echo "$(WARNC)Pushing custom provisioner Docker image $(tag) to the registry...$(NC)"
	docker push $(IMG)

# Release all steps (build, image, push)
release-custom-provisioner: build-custom-provisioner image-custom-provisioner push-custom-provisioner

# Release all targets
release-all: release-custom-provisioner

# Clean build files
clean:
	@echo "$(WARNC)Cleaning up binary files...$(NC)"
	rm -rf $(BIN_DIR)/custom-provisioner
