GO ?= go
CMD ?= single
BIN_DIR ?= build
APP_BIN := $(BIN_DIR)/goclaw

.PHONY: fmt vet test test-race build run run-sender run-worker run-scheduler clean

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
	$(APP_BIN) $(CMD)

run: build

run-sender: build
	$(APP_BIN) term

run-worker: build
	$(APP_BIN) worker

run-scheduler: build
	$(APP_BIN) scheduler

clean:
	rm -rf $(BIN_DIR)
