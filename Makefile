GO ?= go
BIN ?= build/tu

LDFLAGS ?= -s -w

.DEFAULT_GOAL := $(BIN)

$(BIN): *.go go.mod go.sum
	mkdir -p build
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) .

# Convenience alias so `make tu` always delegates to the real build target.
tu: $(BIN)

deps:
	$(GO) mod tidy

clean:
	rm -rf build

.PHONY: clean tu deps
