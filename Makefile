GO := $(shell command -v go 2>/dev/null || echo $(CURDIR)/.tools/go/bin/go)
export GOCACHE := $(CURDIR)/.tools/go-cache

.PHONY: build fmt vet check
build:
	$(GO) build -trimpath -o blindspot .
fmt:
	$(GO) fmt ./...
vet:
	$(GO) vet ./...
check: vet build
