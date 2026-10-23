# lares: run coding agents in a coi sandbox.
#
#   make build     build the lares binary in the repository
#   make install   install it in ~/.local/bin and install the coi profile
#   make dist      build the release archives and their checksums in dist/
#   make test      run the unit tests
#   make check     gofmt, vet, tests

BINARY  := lares
PREFIX  ?= $(HOME)/.local
BIN_DIR := $(PREFIX)/bin

GO      ?= go
GOFLAGS ?=

# The release the binary reports. A repository without tags, or a tree built by
# hand, reports "dev".
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

DIST      := dist
PLATFORMS := linux/amd64 linux/arm64

.PHONY: build install test vet fmt check clean dist

build:
	$(GO) build $(GOFLAGS) -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/lares

install: build
	install -d -m 0755 $(BIN_DIR)
	install -m 0755 $(BINARY) $(BIN_DIR)/$(BINARY)
	$(BIN_DIR)/$(BINARY) install

# dist builds one archive per platform, each holding the static binary, the
# README and install.sh, plus the checksums install.sh verifies. Upload the
# whole directory as the assets of a release tagged VERSION:
#
#   make dist && gh release create v0.1.0 dist/* --title v0.1.0
dist:
	@rm -rf $(DIST) && mkdir -p $(DIST)/stage
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath \
			-ldflags "-s -w -X main.version=$(VERSION)" \
			-o $(DIST)/stage/$(BINARY) ./cmd/lares || exit 1; \
		cp README.md install.sh $(DIST)/stage/; \
		tar -czf $(DIST)/$(BINARY)_$(VERSION)_$${os}_$${arch}.tar.gz -C $(DIST)/stage . || exit 1; \
	done
	@rm -rf $(DIST)/stage
	@cp install.sh $(DIST)/
	@cd $(DIST) && sha256sum *.tar.gz > SHA256SUMS
	@ls -l $(DIST) && echo && cat $(DIST)/SHA256SUMS

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
	rm -rf $(BINARY) $(DIST)
