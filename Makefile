# GoTok Makefile
# Common dev tasks for the TikTok-style Go web app.

BINARY   := gotok
PKG      := .
ADDR     := :8080
DATA_DIR := data

# Phony targets = commands, not files.
.PHONY: help run build vet lint fmt tidy test clean reset serve

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	     /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

run: ## Run the app with go run (http://localhost:8080)
	go run $(PKG)

build: ## Compile a binary to ./$(BINARY)
	go build -o $(BINARY) $(PKG)

serve: build ## Build, then run the compiled binary
	./$(BINARY)

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint (install: brew install golangci-lint)
	golangci-lint run

fmt: ## Format Go source
	gofmt -s -w .

tidy: ## Sync dependencies (go mod tidy)
	go mod tidy

test: ## Run unit tests
	go test ./...

clean: ## Remove the built binary
	rm -f $(BINARY)

reset: clean ## Remove the binary AND wipe local data (db + uploads)
	rm -rf $(DATA_DIR)
