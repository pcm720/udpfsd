package server

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

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

// Print formatted statistics for a single peer
func (s *Server) printPeerStats(addr string, p *peer) {
	udpfsMetrics := p.GetMetrics()
	udprdmaMetrics := p.GetUDPRDMASession().GetMetrics()

	fmt.Printf("\n=== Peer: %s ===\nLast seen: %s\n", addr, p.lastSeen.Format(time.DateTime))

	// Command counts
	var cmdLines []string
	for msgType, count := range udpfsMetrics.CommandCounts {
		if count > 0 {
			cmdLines = append(cmdLines, fmt.Sprintf("%s: %d", msgType.String(), count))
		}
	}
	sort.Strings(cmdLines)
	fmt.Printf("Commands: %s\n", strings.Join(cmdLines, ", "))

	// Error counts
	var errCount int64
	for _, count := range udpfsMetrics.ErrorCounts {
		errCount += count
	}
	if errCount > 0 {
		fmt.Printf("Errors: %d total\n", errCount)
		for msgType, count := range udpfsMetrics.ErrorCounts {
			if count > 0 {
				fmt.Printf("\t%s: %d\n", msgType.String(), count)
			}
		}
	}

	fmt.Printf(`
Read:  %s @ %s
Write: %s @ %s
Total UDPRDMA packets: RX: %d, TX: %d
Peer resets: %d, Peer NACKs: %d
Retransmits: %d, NACKs: %d, Out-of-order packets: %d
`,
		formatBytes(udpfsMetrics.BytesTx), formatRate(udpfsMetrics.AvgTxThroughput),
		formatBytes(udpfsMetrics.BytesRx), formatRate(udpfsMetrics.AvgRxThroughput),
		udprdmaMetrics.TotalPacketsRx, udprdmaMetrics.TotalPacketsTx,
		udprdmaMetrics.PeerResetCount, udprdmaMetrics.PeerNACKCount,
		udprdmaMetrics.Retransmits, udprdmaMetrics.NACKCount, udprdmaMetrics.UnexpectedSeqNrCount,
	)
}
