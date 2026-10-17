GO := $(shell command -v go 2>/dev/null || echo $(CURDIR)/.tools/go/bin/go)
export GOCACHE := $(CURDIR)/.tools/go-cache

.PHONY: build test integration demo doctor
build:
	$(GO) build -trimpath -o blindspot .
test:
	$(GO) test -race ./...
integration: build
	BLINDSPOT_E2E=1 $(GO) test -race -v ./...
demo: build
	./blindspot run --project examples/vault --all --open
doctor:
	./blindspot doctor --project examples/vault
