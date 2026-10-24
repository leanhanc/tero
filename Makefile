VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOBUILD := CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "$(LDFLAGS)"

.PHONY: build build-e2e web test e2e dev-vm dev-vm-update clean
SHELL := /bin/bash

# The dashboard web app, built into internal/dashboard/dist for the binary to
# embed. Bun is only needed here, never on a server.
web:
	find internal/dashboard/dist -mindepth 1 ! -name .gitkeep -delete
	cd web && bun install --frozen-lockfile && bun run build

# Release binaries for both tested architectures.
build: web
	GOARCH=arm64 $(GOBUILD) -o bin/tero-linux-arm64 ./cmd/tero
	GOARCH=amd64 $(GOBUILD) -o bin/tero-linux-amd64 ./cmd/tero

# The binary the end-to-end suite installs in the test VM, which runs on the
# host's architecture. The e2e tag lets it use the VM's test certificate
# authority and a fixed public IP.
E2E_ARCH ?= $(shell go env GOHOSTARCH)
build-e2e: web
	GOARCH=$(E2E_ARCH) $(GOBUILD) -tags e2e -o bin/tero-e2e-linux-$(E2E_ARCH) ./cmd/tero

test:
	go vet ./...
	go vet -tags e2e ./e2e/...
	go test ./...
	cd web && bun run test

# Recreates the test VM and runs the end-to-end suite against it. Output also
# goes to .tero-e2e.log so a run can be followed with tail -f.
e2e:
	set -o pipefail; scripts/vm-test.sh 2>&1 | tee .tero-e2e.log

# A local VM to try the dashboard at https://tero.localhost. dev-vm starts
# fresh and prints the setup link; dev-vm-update ships a new build into it.
dev-vm:
	scripts/dev-vm.sh

dev-vm-update:
	scripts/dev-vm.sh update

clean:
	rm -rf bin
	find internal/dashboard/dist -mindepth 1 ! -name .gitkeep -delete
