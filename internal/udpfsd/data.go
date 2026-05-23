package udpfsd

import (
	"errors"
	"log"
	"net"
	"time"

	"github.com/pcm720/udpfsd/udpfs"
	"github.com/pcm720/udpfsd/udprdma"
)

func (s *Server) dataHandler() {
	defer s.wg.Done()

	s.dataConn.SetReadBuffer(1 << 20)
	buf := make([]byte, 2048)
	for {
		n, addr, err := s.dataConn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				log.Printf("udpfsd/data: connection has been closed")
				return
			}
			if s.verbose {
				log.Printf("udpfsd/data: read error: %v", err)
			}
			continue
		}
		if n < 6 {
			// UDPFS data packet must be at least 6 bytes long
			continue
		}
		s.handleData(buf[:n], addr)
	}
}

func (s *Server) handleData(data []byte, addr *net.UDPAddr) {
	// Get connection handle
	s.Lock()
	c, ok := s.cMap[addr.AddrPort()]
	if !ok {
		log.Printf("[%s]: creating new connection", addr)
		c = &peer{
			udpfs.NewConnection(
				udprdma.NewSession(*addr, func(addr *net.UDPAddr, data []byte) {
					s.dataConn.WriteToUDP(data, addr)
				}),
				s.fs,
				s.verbose,
				s.logMetrics,
			),
			time.Now(),
		}
		s.cMap[addr.AddrPort()] = c
	} else {
		// Update last seen time
		c.lastSeen = time.Now()
	}
	s.Unlock()

	payload, err := c.GetUDPRDMASession().ProcessDataPacket(data)
	if err != nil {
		if s.verbose {
			log.Printf("udpfsd/data: [%s]: %v", addr, err)
		}
		return
	}

	if payload != nil {
		c.HandlePayload(addr, payload)
	}
}
