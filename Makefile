# udpfsd multi-platform build
# Binaries are written to build/udpfsd-<OS>-<arch> (Windows: .exe suffix)

VERSION := $(shell git describe --always --dirty --tags --exclude nightly 2>/dev/null || echo "unknown")
BUILD_DIR := build
LDFLAGS := -ldflags "-w -s -X main.Version=$(VERSION)"

.PHONY: all clean build-linux-x86 build-linux-amd64 build-linux-arm64 build-linux-armv7 build-linux-armv6 build-linux-mipseb build-linux-mipsel build-linux-riscv64 build-darwin-amd64 build-darwin-arm64 build-windows-x86 build-windows-amd64 build-windows-arm64

all: build-linux-x86 build-linux-amd64 build-linux-arm64 build-linux-armv7 build-linux-armv6 build-linux-mipseb build-linux-mipsel build-linux-riscv64 build-darwin-amd64 build-darwin-arm64 build-windows-x86 build-windows-amd64 build-windows-arm64

clean:
	rm -rf $(BUILD_DIR)

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

build-linux-x86: $(BUILD_DIR)
	GOOS=linux GOARCH=386 go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-linux-x86 ./cmd/udpfsd

build-linux-amd64: $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-linux-amd64 ./cmd/udpfsd

build-linux-arm64: $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-linux-arm64 ./cmd/udpfsd

build-linux-armv7: $(BUILD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-linux-armv7 ./cmd/udpfsd

build-linux-armv6: $(BUILD_DIR)
	GOOS=linux GOARCH=arm GOARM=6 go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-linux-armv6 ./cmd/udpfsd

build-linux-mipseb: $(BUILD_DIR)
	GOOS=linux GOARCH=mips GOMIPS=softfloat go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-linux-mipseb ./cmd/udpfsd

build-linux-mipsel: $(BUILD_DIR)
	GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-linux-mipsel ./cmd/udpfsd

build-linux-riscv64: $(BUILD_DIR)
	GOOS=linux GOARCH=riscv64 go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-linux-riscv64 ./cmd/udpfsd

build-darwin-amd64: $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-macos-amd64 ./cmd/udpfsd

build-darwin-arm64: $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-macos-arm64 ./cmd/udpfsd

build-windows-x86: $(BUILD_DIR)
	GOOS=windows GOARCH=386 go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-windows-x86.exe ./cmd/udpfsd

build-windows-amd64: $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-windows-amd64.exe ./cmd/udpfsd

build-windows-arm64: $(BUILD_DIR)
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/udpfsd-windows-arm64.exe ./cmd/udpfsd
