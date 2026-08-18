package server

import (
	"cmp"
	"net/netip"
	"slices"
	"time"

	"github.com/pcm720/udpfsd/udpfs"
	"github.com/pcm720/udpfsd/udprdma"
)

// PeerMetrics is a point-in-time snapshot of one peer's connection metrics.
type PeerMetrics struct {
	Addr     netip.AddrPort
	LastSeen time.Time
	UDPFS    udpfs.Metrics
	UDPRDMA  udprdma.Metrics
}

// Metrics is a point-in-time snapshot of the server's uptime and per-peer metrics.
type Metrics struct {
	StartTime time.Time
	Uptime    time.Duration
	Peers     []PeerMetrics // sorted by peer address, then port
}

// Stats returns a snapshot of the server's uptime and per-peer metrics.
func (s *Server) Stats() Metrics {
	type peerRef struct {
		conn     *udpfs.Connection
		lastSeen time.Time
		addr     netip.AddrPort
	}

	// Copy peer references under the lock, then snapshot each peer outside it:
	// the per-peer collectors take their own locks and never call back into Server.
	s.Lock()
	refs := make([]peerRef, 0, len(s.cMap))
	for addr, p := range s.cMap {
		refs = append(refs, peerRef{conn: p.Connection, lastSeen: p.lastSeen, addr: addr})
	}
	startTime := s.startTime
	s.Unlock()

	metrics := Metrics{
		StartTime: startTime,
		Peers:     make([]PeerMetrics, 0, len(refs)),
	}
	if !startTime.IsZero() {
		metrics.Uptime = time.Since(startTime)
	}
	for _, r := range refs {
		metrics.Peers = append(metrics.Peers, PeerMetrics{
			Addr:     r.addr,
			LastSeen: r.lastSeen,
			UDPFS:    r.conn.Stats(),
			UDPRDMA:  r.conn.GetUDPRDMASession().Stats(),
		})
	}
	slices.SortFunc(metrics.Peers, func(a, b PeerMetrics) int {
		if c := a.Addr.Addr().Compare(b.Addr.Addr()); c != 0 {
			return c
		}
		return cmp.Compare(a.Addr.Port(), b.Addr.Port())
	})
	return metrics
}
