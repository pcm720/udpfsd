package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"log"

	"github.com/pcm720/udpfsd/internal/fs"
	"github.com/pcm720/udpfsd/internal/fs/compression"
	"github.com/pcm720/udpfsd/internal/udpfsd"
	"github.com/pcm720/udpfsd/udprdma"
)

// Version is set at build time via -ldflags "-X main.Version=..."
var Version string = "unknown"

const defaultFsRoot = "./fsroot"

var (
	root                 = flag.String("fsroot", "", "Root directory to serve files from\nEnvironment variable: FSROOT")
	path                 = flag.String("bdpath", "", "Path to block device/image to serve\nEnvironment variable: BDPATH")
	port                 = flag.Int("port", udprdma.UDPFSPort, "UDP port to listen on for discovery packets\nEnvironment variable: PORT")
	bindIP               = flag.String("bind", "", "Address and port for data connection (e.g. 0.0.0.0:62966 or 192.168.1.1:0)\nEnvironment variable: BIND (default :0 = any port)")
	sectorSize           = flag.Int("sector-size", 512, "Sector size for block device\nEnvironment variable: SECTOR_SIZE")
	readOnly             = flag.Bool("ro", false, "Serve in read-only mode\nEnvironment variable: RO")
	verbose              = flag.Bool("verbose", false, "Verbose output\nEnvironment variable: VERBOSE")
	logMetrics           = flag.Bool("metrics", false, "Log metrics\nEnvironment variable: METRICS")
	logMetricsPeriod     = flag.String("metrics-period", "", "Metric logging period in Go time.Duration format\nEnvironment variable: METRICS_PERIOD (default 1m = 1 minute)")
	disableCompression   = flag.Bool("no-compression", false, fmt.Sprintf("Disable transparent decompression for %s\nEnvironment variable: NO_COMPRESSION", strings.Join(compression.GetSupportedFormats(), ", ")))
	compressionCacheSize = flag.Int("compression-cache-size", 32, "Number of decompressed blocks to cache per file\nEnvironment variable: COMPRESSION_CACHE_SIZE")
	peerTimeout          = flag.String("peer-timeout", "", "Time before inactive peer gets removed in Go time.Duration format\nEnvironment variable: PEER_TIMEOUT (default 1h = 1 hour)")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "udpfsd - UDPFS and UDPRDMA server\nVersion: %s\n\n", Version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nAt least one of -fsroot or -bdpath is required.\n")
	}
	flag.Parse()
	loadEnvironment()

	if *path == "" && *root == "" {
		*root = defaultFsRoot
	}

	// Build FS backend options
	fsopts := []fs.BackendOptFunc{
		fs.WithFSRoot(*root),
		fs.WithBlockDevice(*path),
		fs.WithSectorSize(*sectorSize),
		fs.WithCompressionCacheSize(*compressionCacheSize),
	}
	if *readOnly {
		fsopts = append(fsopts, fs.WithReadOnly())
	}
	if !*disableCompression {
		fsopts = append(fsopts, fs.WithCompression())
	}
	// Initialize filesystem backend
	fsbackend, err := fs.NewBackend(fsopts...)
	if err != nil {
		log.Printf("failed to initialize filesystem: %v\n\n", err)
		flag.Usage()
		os.Exit(1)
	}

	var metricsPeriod time.Duration
	if *logMetricsPeriod != "" {
		if metricsPeriod, err = time.ParseDuration(*logMetricsPeriod); err != nil {
			metricsPeriod = 0
		}
	}
	var peerTimeoutDuration time.Duration
	if *peerTimeout != "" {
		if peerTimeoutDuration, err = time.ParseDuration(*peerTimeout); err != nil {
			peerTimeoutDuration = 0
		}
	}

	// Build server options
	opts := []udpfsd.ServerOptFunc{
		udpfsd.WithDiscoveryPort(*port),
		udpfsd.WithDataIP(*bindIP),
		udpfsd.WithFS(fsbackend),
		udpfsd.WithPeerTimeout(peerTimeoutDuration),
	}
	if *verbose {
		opts = append(opts, udpfsd.WithVerbose())
	}
	if *logMetrics {
		opts = append(opts, udpfsd.WithMetrics(metricsPeriod))
	}
	// Initialize server
	server, err := udpfsd.New(opts...)
	if err != nil {
		log.Fatalf("failed to initialize server: %v", err)
	}

	if err := server.Start(); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
	defer server.Close()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	log.Printf("%s signal received, shutting down gracefully", <-sig)
}

func loadEnvironment() {
	// Apply environment variable overrides for flags
	if value := envVarLookup("fsroot", *root); value != "" {
		*root = value
	}
	if value := envVarLookup("bdpath", *path); value != "" {
		*path = value
	}
	if value := envVarLookup("port", ""); value != "" {
		if portVal, err := strconv.Atoi(value); err == nil {
			*port = portVal
		}
	}
	if value := envVarLookup("bind", *bindIP); value != "" {
		*bindIP = value
	}
	if value := envVarLookup("sector-size", ""); value != "" {
		if sectorVal, err := strconv.Atoi(value); err == nil {
			*sectorSize = sectorVal
		}
	}
	if value := envVarLookup("ro", ""); value != "" {
		if roVal, err := strconv.ParseBool(value); err == nil {
			*readOnly = roVal
		}
	}
	if value := envVarLookup("verbose", ""); value != "" {
		if verboseVal, err := strconv.ParseBool(value); err == nil {
			*verbose = verboseVal
		}
	}
	if value := envVarLookup("metrics", ""); value != "" {
		if metricsVal, err := strconv.ParseBool(value); err == nil {
			*logMetrics = metricsVal
		}
	}
	if value := envVarLookup("metrics-period", ""); value != "" {
		*logMetricsPeriod = value
	}
	if value := envVarLookup("no-compression", ""); value != "" {
		if compressionVal, err := strconv.ParseBool(value); err == nil {
			*disableCompression = compressionVal
		}
	}
	if value := envVarLookup("compression-cache-size", ""); value != "" {
		if cacheSizeVal, err := strconv.Atoi(value); err == nil {
			*compressionCacheSize = cacheSizeVal
		}
	}
	if value := envVarLookup("peer-timeout", ""); value != "" {
		*peerTimeout = value
	}
}

func envVarLookup(key string, defaultValue string) string {
	if value := os.Getenv(strings.ToUpper(strings.ReplaceAll(key, "-", "_"))); value != "" {
		return value
	}
	return defaultValue
}
