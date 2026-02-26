GO ?= go
CMD ?= single
BIN_DIR ?= build
APP_BIN := $(BIN_DIR)/goclaw

.PHONY: fmt vet test test-race build run run-term run-worker run-scheduler sloccount clean

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

sloccount:
	@if command -v cloc >/dev/null 2>&1; then \
		cloc --vcs=git --exclude-dir=build,data,vendor .; \
	else \
		echo "cloc not found; using wc -l fallback"; \
		find . -type f \( -name '*.go' -o -name '*.md' -o -name '*.toml' -o -name 'Makefile' \) \
			-not -path './.git/*' -not -path './build/*' -not -path './data/*' -not -path './vendor/*' \
			-print0 | xargs -0 wc -l; \
	fi

clean:
	rm -rf $(BIN_DIR)
