GO ?= go
CMD ?= single
BIN_DIR ?= build
APP_BIN := $(BIN_DIR)/goclaw

.PHONY: fmt vet test test-race build run run-term run-worker run-scheduler clean

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(APP_BIN) ./cmd/goclaw

run:
	$(APP_BIN) run --mode=$(CMD)

run: build

run-term: build
	$(APP_BIN) run --mode=term,scheduler

run-worker: build
	$(APP_BIN) run --mode=worker

run-scheduler: build
	$(APP_BIN) run --mode=scheduler

clean:
	rm -rf $(BIN_DIR)
