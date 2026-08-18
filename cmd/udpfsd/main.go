package main

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pcm720/udpfsd/fs"
	"github.com/pcm720/udpfsd/fs/compression"
	"github.com/pcm720/udpfsd/server"
	"github.com/pcm720/udpfsd/udprdma"
)

// Version is set at build time via -ldflags "-X main.Version=..."
var Version string = "unknown"

const (
	defaultFsRoot        = "./fsroot"
	defaultMetricsPeriod = time.Minute
)

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

	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	// Build FS backend options
	fsopts := []fs.BackendOptFunc{
		fs.WithFSRoot(*root),
		fs.WithBlockDevice(*path),
		fs.WithSectorSize(*sectorSize),
		fs.WithCompressionCacheSize(*compressionCacheSize),
		fs.WithLogger(logger),
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
	logFSInfo(logger, fsbackend.Stats())

	var metricsPeriod time.Duration
	if *logMetricsPeriod != "" {
		if metricsPeriod, err = time.ParseDuration(*logMetricsPeriod); err != nil {
			metricsPeriod = 0
		}
	}
	if metricsPeriod <= 0 {
		metricsPeriod = defaultMetricsPeriod
	}
	var peerTimeoutDuration time.Duration
	if *peerTimeout != "" {
		if peerTimeoutDuration, err = time.ParseDuration(*peerTimeout); err != nil {
			peerTimeoutDuration = 0
		}
	}

	// Build server options
	opts := []server.ServerOptFunc{
		server.WithDiscoveryPort(*port),
		server.WithDataIP(*bindIP),
		server.WithFS(fsbackend),
		server.WithPeerTimeout(peerTimeoutDuration),
		server.WithLogger(logger),
	}
	// Initialize server
	srv, err := server.New(opts...)
	if err != nil {
		log.Fatalf("failed to initialize server: %v", err)
	}

	if err := srv.Start(); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close()

	// Periodically print server statistics to stdout
	if *logMetrics {
		stopMetrics := make(chan struct{})
		go func() {
			ticker := time.NewTicker(metricsPeriod)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					printStats(srv.Stats())
				case <-stopMetrics:
					return
				}
			}
		}()
		defer close(stopMetrics)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	log.Printf("%s signal received, shutting down gracefully", <-sig)
}

// logFSInfo logs the mounted filesystem and block device configuration.
func logFSInfo(logger *slog.Logger, info fs.Metrics) {
	if info.FSRoot != "" {
		logger.Info("fs: mounted root filesystem", "path", info.FSRoot, "read_only", info.ReadOnly)
	}
	if info.BlockDevice != "" {
		logger.Info("fs: mounted block device", "path", info.BlockDevice,
			"sectors", info.TotalSectors, "sector_size", info.SectorSize, "read_only", info.ReadOnly)
	}
	if info.CompressionFormats != nil {
		logger.Info("fs: enabled decompression", "formats", strings.Join(info.CompressionFormats, ", "))
	}
}

// printStats prints a server metrics snapshot to stdout.
func printStats(stats server.Metrics) {
	fmt.Printf("\n"+
		"========== Server statistics (%s) ==========\n"+
		"Uptime: %s\n",
		time.Now().Format(time.DateTime), stats.Uptime.Round(time.Second))

	for i := range stats.Peers {
		printPeerMetrics(&stats.Peers[i])
	}
	fmt.Println("=============================================================")
}

// printPeerMetrics prints formatted metrics for a single peer.
func printPeerMetrics(p *server.PeerMetrics) {
	fmt.Printf("\n=== Peer: %s ===\nLast seen: %s\n", p.Addr.String(), p.LastSeen.Format(time.DateTime))

	// Command counts
	var cmdLines []string
	for msgType, count := range p.UDPFS.CommandCounts {
		if count > 0 {
			cmdLines = append(cmdLines, fmt.Sprintf("%s: %d", msgType.String(), count))
		}
	}
	sort.Strings(cmdLines)
	fmt.Printf("Commands: %s\n", strings.Join(cmdLines, ", "))

	// Error counts
	var errCount int64
	for _, count := range p.UDPFS.ErrorCounts {
		errCount += count
	}
	if errCount > 0 {
		fmt.Printf("Errors: %d total\n", errCount)
		for msgType, count := range p.UDPFS.ErrorCounts {
			if count > 0 {
				fmt.Printf("\t%s: %d\n", msgType.String(), count)
			}
		}
	}

	fmt.Printf("\n"+
		"Read:  %s @ %s\n"+
		"Write: %s @ %s\n"+
		"Total UDPRDMA packets: RX: %d, TX: %d\n"+
		"Peer resets: %d, Peer NACKs: %d\n"+
		"Retransmits: %d, NACKs: %d, Out-of-order packets: %d\n",
		formatBytes(p.UDPFS.BytesTx), formatRate(p.UDPFS.AvgTxThroughput),
		formatBytes(p.UDPFS.BytesRx), formatRate(p.UDPFS.AvgRxThroughput),
		p.UDPRDMA.TotalPacketsRx, p.UDPRDMA.TotalPacketsTx,
		p.UDPRDMA.PeerResetCount, p.UDPRDMA.PeerNACKCount,
		p.UDPRDMA.Retransmits, p.UDPRDMA.NACKCount, p.UDPRDMA.UnexpectedSeqNrCount,
	)
}

// Format bytes to human readable string (B, KB, MB, TB only)
func formatBytes(bytes int64) string {
	const unit = 1024.0
	switch {
	case bytes < unit:
		return fmt.Sprintf("%d B", bytes)
	case bytes < unit*unit:
		return fmt.Sprintf("%.1f KB", float64(bytes)/unit)
	case bytes < unit*unit*unit:
		return fmt.Sprintf("%.2f MB", float64(bytes)/(unit*unit))
	}
	return fmt.Sprintf("%.3f GB", float64(bytes)/(unit*unit*unit))
}

// Format throughput rate to human readable string (B/s, KB/s, MB/s only)
func formatRate(rate float64) string {
	const unit = 1024.0

	if rate < unit {
		return fmt.Sprintf("%.0f B/s", rate)
	} else if rate < unit*unit {
		return fmt.Sprintf("%.1f KB/s", rate/unit)
	}

	return fmt.Sprintf("%.2f MB/s", rate/(unit*unit))
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
