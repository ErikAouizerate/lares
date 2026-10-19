# lares: run coding agents in a coi sandbox.
#
#   make build     build the lares binary in the repository
#   make install   install it in ~/.local/bin and install the coi profile
#   make test      run the unit tests
#   make check     gofmt, vet, tests

BINARY  := lares
PREFIX  ?= $(HOME)/.local
BIN_DIR := $(PREFIX)/bin

GO      ?= go
GOFLAGS ?=

.PHONY: build install test vet fmt check clean

build:
	$(GO) build $(GOFLAGS) -o $(BINARY) ./cmd/lares

install: build
	install -d -m 0755 $(BIN_DIR)
	install -m 0755 $(BINARY) $(BIN_DIR)/$(BINARY)
	$(BIN_DIR)/$(BINARY) install

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w .

check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run 'make fmt'"; exit 1; }
	$(GO) vet ./...
	$(GO) test ./...

clean:
	rm -f $(BINARY)
