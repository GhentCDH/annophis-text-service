APP_NAME := annophis-text-service
CMD_DIR  := cmd/annophis-text-service
BIN_DIR  := bin
GO       := go

PORT     ?= 8080
TAG      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
REGISTRY ?=
IMAGE    := $(if $(REGISTRY),$(REGISTRY)/, )$(APP_NAME):$(TAG)

GOFLAGS  := -trimpath
LDFLAGS  := -s -w

.PHONY: help all build run test fmt vet clean tidy deps docker-build docker-run docker-push docker-cross

help: ## Show this help
	@awk 'BEGIN {FS := ":.*##"; printf "\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: build ## Default: build

$(BIN_DIR)/$(APP_NAME): ## Build binary into ./bin
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $@ ./$(CMD_DIR)

build: $(BIN_DIR)/$(APP_NAME) ## Build

run: ## Run locally
	ORIGIN_ALLOWED=http://localhost:5173 \
	CONFIG=./config.json \
	$(GO) run ./$(CMD_DIR)

test: ## Run tests
	$(GO) test ./...

fmt: ## go fmt
	$(GO) fmt ./...

vet: ## go vet
	$(GO) vet ./...

clean: ## Clean build artifacts
	rm -rf $(BIN_DIR)

tidy: ## go mod tidy
	$(GO) mod tidy

deps: ## Update deps
	$(GO) get ./...

docker-build: ## Build Docker image
	docker build -t $(IMAGE) .

docker-run: ## Run Docker container (maps $(PORT))
	docker run --rm -p $(PORT):8080 \
		-e ORIGIN_ALLOWED=http://localhost:5173 \
		-v $(PWD)/config.json:/app/config.json:ro \
		--name $(APP_NAME) $(IMAGE)

docker-push: docker-build ## Push Docker image (set REGISTRY=…)
	docker push $(IMAGE)

docker-cross: ## Multi-arch build (requires buildx)
	docker buildx build --platform linux/amd64,linux/arm64 \
		-t $(IMAGE) --push .
