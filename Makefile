GO := $(shell command -v go 2>/dev/null || echo $(CURDIR)/.tools/go/bin/go)
export GOCACHE := $(CURDIR)/.tools/go-cache

.PHONY: build install release fmt vet check
build:
	$(GO) build -trimpath -o blindspot .
install:
	$(GO) install -trimpath .
release:
	GO="$(GO)" sh scripts/release.sh "$(VERSION)"
fmt:
	$(GO) fmt ./...
vet:
	$(GO) vet ./...
check: vet build
