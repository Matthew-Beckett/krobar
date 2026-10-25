MODULE   := github.com/Matthew-Beckett/krobar
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -X $(MODULE)/internal/version.Version=$(VERSION) \
            -X $(MODULE)/internal/version.Commit=$(COMMIT) \
            -X $(MODULE)/internal/version.Date=$(DATE)

GORELEASER := go run github.com/goreleaser/goreleaser/v2@v2.18.3

.PHONY: build test cover lint fmt vet tidy clean snapshot release-check

build:
	go build -ldflags "$(LDFLAGS)" -o bin/krobar ./cmd/krobar

test:
	go test -race ./...

cover:
	go test -race -coverprofile=cover.out ./...
	go tool cover -func=cover.out

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run

fmt:
	gofmt -w .
	goimports -w -local $(MODULE) .

vet:
	go vet ./...

tidy:
	go mod tidy

snapshot:
	$(GORELEASER) release --snapshot --clean

release-check:
	$(GORELEASER) check

clean:
	rm -rf bin dist cover.out
