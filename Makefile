BINARY  := mcp-sentinel
CMD     := ./cmd/sentinel
OUT_DIR := bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -s -w"

.PHONY: help build build-linux build-darwin build-windows build-all test vet clean install

# ── Default ────────────────────────────────────────────────────────────────────

help:
	@echo ""
	@echo "  MCP Sentinel — Build targets"
	@echo ""
	@echo "  make build              Build for current OS/arch  →  bin/mcp-sentinel"
	@echo "  make build-linux        Linux   amd64 + arm64"
	@echo "  make build-darwin       macOS   amd64 + arm64"
	@echo "  make build-windows      Windows amd64"
	@echo "  make build-all          All platforms"
	@echo ""
	@echo "  make test               go test -race ./..."
	@echo "  make vet                go vet ./..."
	@echo "  make install            Copy binary to /usr/local/bin"
	@echo "  make clean              Remove bin/"
	@echo ""

# ── Local build ────────────────────────────────────────────────────────────────

build:
	@mkdir -p $(OUT_DIR)
	go build $(LDFLAGS) -o $(OUT_DIR)/$(BINARY) $(CMD)
	@echo "→ $(OUT_DIR)/$(BINARY)"

# ── Cross-compilation ──────────────────────────────────────────────────────────

build-linux:
	@mkdir -p $(OUT_DIR)
	GOOS=linux  GOARCH=amd64 go build $(LDFLAGS) -o $(OUT_DIR)/$(BINARY)-linux-amd64  $(CMD)
	GOOS=linux  GOARCH=arm64 go build $(LDFLAGS) -o $(OUT_DIR)/$(BINARY)-linux-arm64  $(CMD)
	@echo "→ $(OUT_DIR)/$(BINARY)-linux-{amd64,arm64}"

build-darwin:
	@mkdir -p $(OUT_DIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(OUT_DIR)/$(BINARY)-darwin-amd64 $(CMD)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(OUT_DIR)/$(BINARY)-darwin-arm64 $(CMD)
	@echo "→ $(OUT_DIR)/$(BINARY)-darwin-{amd64,arm64}"

build-windows:
	@mkdir -p $(OUT_DIR)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(OUT_DIR)/$(BINARY)-windows-amd64.exe $(CMD)
	@echo "→ $(OUT_DIR)/$(BINARY)-windows-amd64.exe"

build-all: build-linux build-darwin build-windows
	@echo ""
	@echo "All builds complete:"
	@ls -lh $(OUT_DIR)/

# ── Quality ────────────────────────────────────────────────────────────────────

test:
	go test -race ./...

vet:
	go vet ./...

# ── Install / Clean ────────────────────────────────────────────────────────────

install: build
	cp $(OUT_DIR)/$(BINARY) /usr/local/bin/$(BINARY)
	@echo "→ installed to /usr/local/bin/$(BINARY)"

clean:
	rm -rf $(OUT_DIR)
	@echo "→ bin/ removed"
