// UDPFS server implementation
package udpfsd

import (
	"context"
	"fmt"
	"log"
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

	port                 int
	metricsLoggingPeriod time.Duration

	sync.Mutex
	verbose    bool
	logMetrics bool
}

type peer struct {
	*udpfs.Connection
	lastSeen time.Time
}

const (
	defaultPeerTimeout   = time.Hour
	peerCleanupInterval  = 30 * time.Second
	metricsLoggingPeriod = time.Minute
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
func WithVerbose() func(s *Server) {
	return func(s *Server) {
		s.verbose = true
	}
}

func WithFS(fs udpfs.FS) func(s *Server) {
	return func(s *Server) {
		if fs != nil {
			s.fs = fs
		}
	}
}

func WithMetrics(loggingPeriod time.Duration) func(s *Server) {
	return func(s *Server) {
		if loggingPeriod > 0 {
			s.metricsLoggingPeriod = loggingPeriod
		}
		s.logMetrics = true
	}
}

func WithPeerTimeout(peerTimeout time.Duration) func(s *Server) {
	return func(s *Server) {
		if peerTimeout > 0 {
			log.Printf("udpfsd: setting peer timeout to %s", peerTimeout)
			s.peerTimeout = peerTimeout
		}
	}
}

// Creates new udpfsd server
func New(opts ...ServerOptFunc) (*Server, error) {
	s := &Server{
		port:                 udprdma.UDPFSPort,
		cMap:                 make(map[netip.AddrPort]*peer),
		logMetrics:           false,
		metricsLoggingPeriod: metricsLoggingPeriod,
		peerTimeout:          defaultPeerTimeout,
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

	s.startTime = time.Now()

	s.wg.Add(2)
	go s.discoveryHandler()
	log.Printf("udpfsd: listening for incoming discovery packets on %s", s.discConn.LocalAddr())
	go s.dataHandler()
	log.Printf("udpfsd: listening for incoming data packets on %s", s.dataConn.LocalAddr())
	go s.cleanup()

	if s.logMetrics {
		go s.metricsLogger()
	}
	return nil
}

func (s *Server) Close() {
	s.dataConn.Close()
	s.discConn.Close()
	s.wg.Wait()
}

func (s *Server) cleanup() {
	for range time.Tick(peerCleanupInterval) {
		s.Lock()
		for pAddr, p := range s.cMap {
			if time.Since(p.lastSeen) >= s.peerTimeout {
				log.Printf("udpfsd: peer %s hasn't been seen for more than %s, removing", pAddr, s.peerTimeout)
				p.Connection.Close()
				p.Connection = nil
				delete(s.cMap, pAddr)
			}
		}
		s.Unlock()
	}
}

func (s *Server) metricsLogger() {
	for range time.Tick(s.metricsLoggingPeriod) {
		s.Lock()
		fmt.Printf(`
========== Server statistics (%s) ==========
Uptime: %s
`, time.Now().Format(time.DateTime), time.Since(s.startTime).Round(time.Second))

		for pAddr, p := range s.cMap {
			s.printPeerStats(pAddr.String(), p)
		}
		fmt.Println("=============================================================")
		s.Unlock()
	}
}
