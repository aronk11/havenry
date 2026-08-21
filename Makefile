BIN     := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build agent controlplane test lint size clean devfake

all: build

build: agent controlplane

## Der Agent bleibt CGO-frei: er laeuft auf vielen kleinen Hosts, wo statische
## Binaries und einfaches Cross-Compiling nach arm64 zaehlen.
agent:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/havenry-agent ./cmd/havenry-agent

## Die Control Plane braucht derzeit CGO wegen des SQLite-Treibers.
## Das ist eine bekannte, dokumentierte Uebergangsloesung: nach dem Wechsel auf
## modernc.org/sqlite (ADR-0020) faellt CGO_ENABLED=1 wieder weg.
controlplane:
	CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/havenry ./cmd/havenry

## Die CLI ist CGO-frei — sie wird auf Arbeitsrechnern installiert,
## nicht nur auf dem Server.
cli:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/havenryctl ./cmd/havenryctl

test:
	CGO_ENABLED=1 go test ./... -race -count=1

lint:
	golangci-lint run ./...

## Startet einen Docker-Daemon-Ersatz fuer manuelle Tests ohne installiertes Docker.
devfake:
	go run scripts/devfake/main.go /tmp/fake-docker.sock

## Prueft das Groessenbudget aus ADR-0005 (Agent < 20 MB)
size: agent
	@s=$$(stat -c%s $(BIN)/havenry-agent); \
	echo "agent: $$((s/1024/1024)) MB"; \
	if [ $$s -gt 20971520 ]; then echo "FEHLER: Agent ueberschreitet 20 MB (ADR-0005)"; exit 1; fi

clean:
	rm -rf $(BIN) dist
