package server

import (
	"errors"
	"net"
	"time"

	"github.com/pcm720/udpfsd/udpfs"
	"github.com/pcm720/udpfsd/udprdma"
)

func (s *Server) dataHandler() {
	defer s.wg.Done()

	_ = s.dataConn.SetReadBuffer(1 << 20)
	_ = s.dataConn.SetWriteBuffer(1 << 20)
	buf := make([]byte, 2048)
	for {
		n, addr, err := s.dataConn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				s.logger.Info("data connection has been closed")
				return
			}
			s.logger.Debug("data read error", "err", err)
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
		s.logger.Info("creating new connection", "peer", addr)
		conn := s.dataConn
		writeTo := func(a *net.UDPAddr, payload []byte) {
			_, _ = conn.WriteToUDPAddrPort(payload, a.AddrPort())
		}
		c = &peer{
			udpfs.NewConnection(
				udprdma.NewSession(*addr, writeTo, newUDPBatchWriter(conn), udprdma.WithLogger(s.logger)),
				s.fs,
				udpfs.WithLogger(s.logger),
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
		s.logger.Debug("failed to process data packet", "peer", addr, "err", err)
		return
	}

	if payload != nil {
		c.HandlePayload(addr, payload)
	}
}
