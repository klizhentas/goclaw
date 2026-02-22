GO ?= go
MODE ?= single
BIN_DIR ?= build
APP_BIN := $(BIN_DIR)/miniclaw

.PHONY: fmt test test-race build run run-sender run-worker clean

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(APP_BIN) ./cmd/miniclaw

run:
	$(APP_BIN) -mode $(MODE)

run: build

run-sender: build
	$(APP_BIN) -mode sender

run-worker: build
	$(APP_BIN) -mode worker

clean:
	rm -rf $(BIN_DIR)
