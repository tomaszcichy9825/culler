# Standard entry points. Wails' own task runner (Taskfile.yml) does the heavy
# lifting; this is the familiar front door.

GOBIN := $(shell go env GOPATH)/bin
export PATH := $(PATH):$(GOBIN)

.PHONY: all build dev run test test-race fuzz vet fmt check clean package release tools help

all: build

help: ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

tools: ## install the wails3 CLI
	go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.2

build: ## build the app binary into bin/
	wails3 build

dev: ## run with hot reload
	wails3 dev

run: build ## build then launch the app
	./bin/culler

test: ## run Go tests
	go test ./internal/...

test-race: ## run Go tests with the race detector
	go test -race ./internal/...

fuzz: ## run preview-extractor fuzzing for 30s
	go test -fuzz=Fuzz -fuzztime=30s ./internal/preview/

vet: ## go vet
	go vet ./internal/... .

fmt: ## gofmt all Go code
	gofmt -w cmd internal *.go 2>/dev/null || gofmt -w internal *.go

check: vet test-race ## what CI runs: vet + race tests + frontend check
	cd frontend && npm run check

package: ## production package for this platform (unsigned)
	wails3 package

release: package ## local unsigned release artefact for this platform

clean: ## remove build outputs
	rm -rf bin frontend/dist
