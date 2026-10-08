VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOBUILD := CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "$(LDFLAGS)"

.PHONY: build build-e2e test e2e clean
SHELL := /bin/bash

# Release binaries for both tested architectures.
build:
	GOARCH=arm64 $(GOBUILD) -o bin/tero-linux-arm64 ./cmd/tero
	GOARCH=amd64 $(GOBUILD) -o bin/tero-linux-amd64 ./cmd/tero

# The binary the end-to-end suite installs in the test VM. The e2e tag lets it
# use the VM's test certificate authority and a fixed public IP.
build-e2e:
	GOARCH=arm64 $(GOBUILD) -tags e2e -o bin/tero-e2e-linux-arm64 ./cmd/tero

test:
	go test ./...

# Recreates the test VM and runs the end-to-end suite against it. Output also
# goes to .tero-e2e.log so a run can be followed with tail -f.
e2e:
	set -o pipefail; scripts/vm-test.sh 2>&1 | tee .tero-e2e.log

clean:
	rm -rf bin
