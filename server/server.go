// UDPFS server implementation
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/pcm720/udpfsd/udpfs"
	"github.com/pcm720/udpfsd/udprdma"
)

// Server is the UDPFS daemon: sockets, UDPRDMA session, and dispatch to udpfs.FS.
type Server struct {
	startTime   time.Time
	peerTimeout time.Duration

	// FS is the filesystem implementation; the protocol layer (udpfs) parses packets and calls FS.
	fs udpfs.FS

	// Connections
	discConn *net.UDPConn
	dataConn *net.UDPConn

	// Known peers
	cMap map[netip.AddrPort]*peer

	bindIP string
	wg     sync.WaitGroup

	port int

	// stopCh is closed by Close to stop background goroutines
	stopCh   chan struct{}
	stopOnce sync.Once

	sync.Mutex
	logger *slog.Logger
}

type peer struct {
	*udpfs.Connection
	lastSeen time.Time
}

const (
	defaultPeerTimeout  = time.Hour
	peerCleanupInterval = 30 * time.Second
)

type ServerOptFunc func(s *Server)

func WithDiscoveryPort(port int) func(s *Server) {
	return func(s *Server) {
		if port != 0 {
			s.port = port
		}
	}
}

func WithDataIP(ip string) func(s *Server) {
	return func(s *Server) {
		if ip != "" {
			s.bindIP = ip
		}
	}
}

// WithLogger sets the server logger. The logger is passed down to per-peer
// connections and UDPRDMA sessions. By default, log output is discarded.
func WithLogger(l *slog.Logger) ServerOptFunc {
	return func(s *Server) {
		if l != nil {
			s.logger = l
		}
	}
}

func WithFS(fs udpfs.FS) func(s *Server) {
	return func(s *Server) {
		if fs != nil {
			s.fs = fs
		}
	}
}

func WithPeerTimeout(peerTimeout time.Duration) func(s *Server) {
	return func(s *Server) {
		if peerTimeout > 0 {
			s.peerTimeout = peerTimeout
		}
	}
}

// Creates new udpfsd server
func New(opts ...ServerOptFunc) (*Server, error) {
	s := &Server{
		port:        udprdma.UDPFSPort,
		cMap:        make(map[netip.AddrPort]*peer),
		peerTimeout: defaultPeerTimeout,
		logger:      slog.New(slog.DiscardHandler),
		stopCh:      make(chan struct{}),
	}
	for _, f := range opts {
		f(s)
	}
	if s.fs == nil {
		return nil, fmt.Errorf("filesystem handler not set")
	}

	return s, nil
}

// Binds discovery and data sockets, creates session and connection and starts packet handlers
func (s *Server) Start() error {
	lc := net.ListenConfig{
		Control: setSocketOptionFunc(),
	}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":"+strconv.Itoa(s.port))
	if err != nil {
		return err
	}

	var ok bool
	if s.discConn, ok = pc.(*net.UDPConn); !ok {
		pc.Close()
		return fmt.Errorf("expected *net.UDPConn, got %T", pc)
	}

	if s.bindIP != "" {
		if _, _, err := net.SplitHostPort(s.bindIP); err != nil {
			s.bindIP = net.JoinHostPort(s.bindIP, "0")
		}
	} else {
		s.bindIP = ":0"
	}
	dataUDP, err := net.ResolveUDPAddr("udp4", s.bindIP)
	if err != nil {
		return err
	}
	s.dataConn, err = net.ListenUDP("udp4", dataUDP)
	if err != nil {
		return err
	}

	s.Lock()
	s.startTime = time.Now()
	s.Unlock()

	s.wg.Add(3)
	go s.discoveryHandler()
	s.logger.Info("listening for incoming discovery packets", "addr", s.discConn.LocalAddr())
	go s.dataHandler()
	s.logger.Info("listening for incoming data packets", "addr", s.dataConn.LocalAddr())
	go s.cleanup()
	return nil
}

// Close stops the packet handlers and the peer cleanup loop, then waits for them to finish.
func (s *Server) Close() {
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.dataConn.Close()
	s.discConn.Close()
	s.wg.Wait()
}

func (s *Server) cleanup() {
	defer s.wg.Done()

	ticker := time.NewTicker(peerCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.Lock()
			for pAddr, p := range s.cMap {
				if time.Since(p.lastSeen) >= s.peerTimeout {
					s.logger.Warn("peer hasn't been seen for more than the timeout, removing", "peer", pAddr, "timeout", s.peerTimeout)
					p.Connection.Close()
					p.Connection = nil
					delete(s.cMap, pAddr)
				}
			}
			s.Unlock()
		}
	}
}
