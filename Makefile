# Standard entry points. Wails' own task runner (Taskfile.yml) does the heavy
# lifting; this is the familiar front door.

# macOS ships make 3.81, which resolves plain commands against its own PATH
# rather than the exported one — so wails3 is always invoked by full path.
GOBIN := $(shell go env GOPATH)/bin
WAILS3 := $(shell command -v wails3 2>/dev/null || echo $(GOBIN)/wails3)
# ...while wails3's own task runner re-invokes `wails3` by name, so the
# children still need GOBIN on PATH.
export PATH := $(PATH):$(GOBIN)

.PHONY: all build dev run test test-race fuzz vet fmt check clean package release tools icons help

all: build

help: ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

tools: ## install the wails3 CLI
	go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.2

build: ## build the app binary into bin/
	"$(WAILS3)" build

dev: ## run with hot reload
	"$(WAILS3)" dev

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

icons: ## rebuild the app icons from assets/brand
	./scripts/icons.sh

package: ## production package for this platform (unsigned)
	"$(WAILS3)" package

release: package ## local unsigned release artefact for this platform

clean: ## remove build outputs
	rm -rf bin frontend/dist
