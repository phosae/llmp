# ko-build configuration for llmp
APP_NAME := llmp
REGISTRY ?= local
IMAGE_TAG ?= latest
KO_DOCKER_REPO := $(REGISTRY)/$(APP_NAME)

# Default target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  install-ko  - Install ko-build tool"
	@echo "  build       - Build Docker image using ko"
	@echo "  build-local - Build and load image to local Docker daemon"
	@echo "  push        - Build and push image to registry"
	@echo "  run         - Build and run the container locally"
	@echo "  clean       - Clean up built images"
	@echo ""
	@echo "Environment variables:"
	@echo "  REGISTRY    - Docker registry (default: local)"
	@echo "  IMAGE_TAG   - Image tag (default: latest)"
	@echo "  KO_DOCKER_REPO - Full repository path (default: $(REGISTRY)/$(APP_NAME))"

# Install ko-build tool
.PHONY: install-ko
install-ko:
ifeq (, $(shell which ko))
	GOBIN=/usr/local/bin/ go install github.com/google/ko@v0.18.0
endif

# Build image using ko
.PHONY: build
build:
	@echo "Building Docker image with ko..."
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build --bare .

# Build and load to local Docker daemon
.PHONY: build-local
build-local:
	@echo "Building and loading Docker image locally..."
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build --bare --local .

# Build and push to registry
.PHONY: push
push:
	@echo "Building and pushing Docker image..."
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build --bare --push .

# Build and run locally
.PHONY: run
run: build-local
	@echo "Running container locally..."
	docker run --rm -p 8400:8400 -v $(PWD)/config.yaml:/app/config.yaml $(KO_DOCKER_REPO):latest

# Clean up images
.PHONY: clean
clean:
	@echo "Cleaning up Docker images..."
	docker rmi -f $(shell docker images $(KO_DOCKER_REPO) -q) 2>/dev/null || true

# Publish with specific tag
.PHONY: release
release:
	@if [ -z "$(TAG)" ]; then echo "Usage: make release TAG=v1.0.0"; exit 1; fi
	@echo "Building and pushing release $(TAG)..."
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build --bare --tags=$(TAG) --push .