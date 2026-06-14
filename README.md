# udpfsd — UDPFS server

A Go implementation of the UDPFS/UDPRDMA server used for serving filesystems and block devices to a PlayStation 2 over the network.

## Overview

### Server Features

- UDPFS and UDPBD (as UDPFS subset) protocols
- Serve a filesystem directory, a block device image, or both at once
- On-the-fly decompression of CSO, ZSO, and CHD disc images
- Multiple concurrent clients
- Read-only mode

This server is based on the original [UDPFS server from Neutrino](https://github.com/rickgaiser/neutrino/blob/master/pc/udpfs_server.py), written in Python.  
The Go port keeps the same protocol and feature set; compared to the original, it adds:

- Isolated session and handle state per client, so several PS2s can use the same server at once.
- Discovery and data handling run in separate goroutines, so one client’s traffic or decompression does not block others.
- More handles (64 vs 32) and automatic cleanup of idle peers and opened files.
- Single binary and environment-variable configuration for containers and embedded devices.

## Usage

### Prerequisites

- A directory containing files to serve (for filesystem mode)
- Optionally, a block device image for UDPBD over UDPRDMA
- `udpfsd` binary (or `udpfsd.exe` on Windows)

Pre-built archives are available in [Releases](https://github.com/pcm720/udpfsd/releases).  
Each archive contains a single binary named `udpfsd` (or `udpfsd.exe` on Windows) plus this README.  
Download the archive for your platform and extract it.

*`<version>` is the release tag (e.g. `v1.0.0`) or `nightly` for development builds.*

| Platform | Architecture               | Release archive                      |
|----------|----------------------------|--------------------------------------|
| Linux    | AMD64                      | `udpfsd-linux-amd64-<version>.zip`   |
| Linux    | ARM 64-bit                 | `udpfsd-linux-arm64-<version>.zip`   |
| Linux    | ARM v7 (32-bit)            | `udpfsd-linux-armv7-<version>.zip`   |
| Linux    | ARM v6 (32-bit)            | `udpfsd-linux-armv6-<version>.zip`   |
| Linux    | MIPS32 BE (softfloat)      | `udpfsd-linux-mipseb-<version>.zip`  |
| Linux    | MIPS32 LE (softfloat)      | `udpfsd-linux-mipsel-<version>.zip`  |
| Linux    | RISC-V (64-bit)            | `udpfsd-linux-riscv64-<version>.zip` |
| macOS    | AMD64                      | `udpfsd-macos-amd64-<version>.zip`   |
| macOS    | ARM 64-bit (Apple Silicon) | `udpfsd-macos-arm64-<version>.zip`   |
| Windows  | AMD64                      | `udpfsd-windows-amd64-<version>.zip` |
| Windows  | ARM 64-bit                 | `udpfsd-windows-arm64-<version>.zip` |

> **Note:** Pre-built binaries support only CSO and ZSO.  
For CHD, build from source with CGO (see [building from source](#building-from-source)).

### Quick start

Serve a directory (Linux/macOS):
```bash
$ udpfsd -fsroot /path/to/files
```

Windows (PowerShell or cmd):
```powershell
> udpfsd.exe -fsroot C:\path\to\files
```

Or serve a block device image:
```bash
$ udpfsd -bdpath /path/to/image.chd
```

Both options can be used at the same time.

At least one of `-fsroot` or `-bdpath` is required; if you omit both, the server will attempt to use `fsroot` directory in its current working directory as filesystem root.  
If this directory does not exist, the server prints an error and exits.

### Configuration

udpfsd supports both command-line options and environment variables.  
Environment variable names are the uppercase form of the flag name with hyphens replaced by underscores:

| Environment Variable | Flag | Description |
|---------------------|--------------------|-------------|
| `FSROOT` | `-fsroot` | Root directory to serve files from (default: `./fsroot`) |
| `BDPATH` | `-bdpath` | Path to block device/image to serve |
| `PORT` | `-port` | UDP port for discovery and data (default: 62966) |
| `BIND` | `-bind` | Address and port for data connection, e.g. `0.0.0.0:62966` or `192.168.1.1:0` (default: `:0` = any port) |
| `SECTOR_SIZE` | `-sector-size` | Sector size for block device in bytes (default: 512) |
| `RO` | `-ro` | Serve in read-only mode |
| `VERBOSE` | `-verbose` | Enable verbose output |
| `METRICS` | `-metrics` | Enable server statistics logging |
| `METRICS_PERIOD` | `-metrics-period` | Metric logging period in Go time.Duration format (default: 1m) |
| `NO_COMPRESSION` | `-no-compression` | Disable transparent decompression for CHD/CSO/ZSO (enabled by default) |
| `COMPRESSION_CACHE_SIZE` | `-compression-cache-size` | Number of cached blocks per file (default: 32) |
| `PEER_TIMEOUT` | `-peer-timeout` | Time before inactive peer gets removed in Go time.Duration format (default: 1h) |

### Limitations

- Only one client may have a given file open for writing at a time.  
  While a file is open for writing, other clients cannot open that file for reading or writing.

### Examples

```bash
# Serve files from a specific directory in read-only mode
$ FSROOT=/mnt/files RO=1 udpfsd
$ udpfsd -fsroot /mnt/files -ro

# Serve without compression disabled and verbose logging (default is compression enabled)
$ FSROOT=/path/to/files NO_COMPRESSION=0 VERBOSE=1 udpfsd
$ udpfsd -fsroot /path/to/files -no-compression -verbose

# Explicitly disable compression:
$ FSROOT=/path/to/files COMPRESSION=0 VERBOSE=1 udpfsd
$ udpfsd -fsroot /path/to/files -no-compression

# Bind to specific interface with read-only mode, serving both directory and block device
$ BDPATH=/mnt/exfat.img FSROOT=/shared BIND=192.168.1.100 RO=1 udpfsd
$ udpfsd -bdpath /mnt/exfat.img -fsroot /shared -bind 192.168.1.100 -ro


# Use environment variables instead of command-line arguments
export FSROOT=/mnt/files
export BDPATH=/images/exfat.img
export COMPRESSION_CACHE_SIZE=64
$ udpfsd
```

## Compression Support

By default, the server enables transparent decompression for **CHD**, **CSO**, and **ZSO** images, serving them as if they were raw disc images.  
On-the-fly decompression is used **only** when the client opens the virtual name produced by directory listings (the real filename with `.iso` appended; see [Opening a file](#opening-a-file)).  
If the client opens the file by its actual name on disk (e.g. `game.cso`), the server returns the compressed file bytes unchanged.

To disable compression support, use the `-no-compression` flag or set `NO_COMPRESSION=1`.

The decompression cache stores recently accessed blocks per file using an LRU strategy.  
The default cache size is 32 blocks, configurable via `COMPRESSION_CACHE_SIZE`.

### Directory listings

When compression support is enabled, if a file is stored as a compressed image (for example `game.cso` or `disc.zso`), the name returned to the client has `.iso` appended (e.g. `game.cso.iso`).  
The reported size is the uncompressed disc size, not the compressed file size on disk.

### Opening a file

When compression support is enabled: when the client opens a path that ends in `.iso` and no file exists at that exact name, the server drops the trailing `.iso` and, if that path names a supported compressed image on disk, serves it **decompressed** (e.g. `game.cso.iso` → underlying `game.cso`).

If the client opens the compressed file by its **real** extension and the file exists at that path, the server opens it as a normal file: reads see the **compressed** contents on disk, not a decompressed ISO.

When compression support is disabled, all files (including compressed images) are served as-is without any decompression behavior.

Plain `.iso` files and other non-compressed files are listed and opened unchanged in both modes.

## Protocol Details

For complete protocol specifications, see:

- [UDPRDMA Protocol](https://github.com/rickgaiser/neutrino/blob/726c22cfa42287cff37d0bcc20d1e0148805632c/iop/udpfs/UDPRDMA.md)
- [UDPFS Protocol](https://github.com/rickgaiser/neutrino/blob/726c22cfa42287cff37d0bcc20d1e0148805632c/iop/udpfs/UDPFS.md)
- [Wireshark dissector](docs/wireshark/)

## Building from source

### Prerequisites

- Go 1.25 or later
- libchdr for CHD support (optional, requires CGO)

### Building with CGO (Required for CHD support)

To build the server **with full CHD support**, you must:

1. Install `libchdr` on your system:
   - **Debian/Ubuntu**: `sudo apt install libchdr0 libchdr-dev`
   - **Arch Linux**: `libchdr-git` from AUR
   - Build from [source](https://github.com/rtissera/libchdr)

2. Enable CGO and build manually:
   ```bash
   export CGO_ENABLED=1
   go build -o bin/udpfsd ./cmd/udpfsd
   ```

### Building without CGO (CSO/ZSO only)

If you don't need CHD support:
```bash
CGO_ENABLED=0 go build -o bin/udpfsd ./cmd/udpfsd
```

To build all release targets (Linux, macOS, Windows; multiple architectures), run `make` from the repository root. Binaries are written to `build/`.

## Troubleshooting

Common issues and fixes. Run the server with `-verbose` to see more detail about discovery and errors in the server logs.

- **"failed to initialize filesystem"** — The server exits with this when `-fsroot` is not a directory or `-bdpath` cannot be opened. Check that paths exist, that the user has read access (and write access if not using `-ro`), and that the block device or image file is not in use elsewhere.

- **CHD not working** — Pre-built binaries do not include CHD support. Build from source with CGO and libchdr (see [Building from source](#building-from-source)).

- **Client can't connect** — Ensure the server discovery port (default 62966) is open in your firewall and that `-bind` matches an 
interface the client can reach. If required, set the port in the `-bind` argument (e.g. `-bind :41233`).

- **"Wrong packet type 2 (expected 0/DISCOVERY)"** (in server logs) — The client may be using an older Neutrino (pre–v1.8.0-13) with an incompatible UDPFS protocol; upgrade to the latest Neutrino. This can also happen if the client is sending data to the discovery port by mistake—check that the client is not configured to use the discovery port for data.

- **Client timeouts / "got unexpected sequence number"** (in server logs) — If the server has multiple interfaces on the same network (e.g., wired and Wi-Fi), the client can receive duplicate packets or bind to the wrong interface. Bind the data connection to a single interface with `-bind` and that interface’s IP (e.g. `-bind 192.168.1.100` or `BIND=192.168.1.100`). Run the server with `-verbose` to see these messages when the client times out.

## License

See [LICENSE](LICENSE).

## Credits

- Rick Gaiser for [Neutrino](https://github.com/rickgaiser/neutrino) and UDPFS
