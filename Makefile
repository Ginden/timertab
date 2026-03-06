BINARY ?= timertab
PKG := ./cmd/timertab

.PHONY: build
build:
	go build -o ./bin/$(BINARY) $(PKG)

.PHONY: install
install:
	go install $(PKG)

.PHONY: run
run:
	go run $(PKG) --help

.PHONY: test
test:
	go test ./...

.PHONY: fmt
fmt:
	gofmt -w cmd/timertab/main.go internal/cli/*.go internal/config/*.go internal/version/*.go
