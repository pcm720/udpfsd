package server

import (
	"errors"
	"net"

	"github.com/pcm720/udpfsd/udprdma"
)

func (s *Server) discoveryHandler() {
	defer s.wg.Done()

	buf := make([]byte, 2048)
	s.discConn.SetReadBuffer(2048)
	for {
		n, addr, err := s.discConn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				s.logger.Info("discovery connection has been closed")
				return
			}
			s.logger.Debug("discovery read error", "err", err)
			continue
		}
		if n < 6 {
			// UDPRDMA discovery packet must be at least 6 bytes long
			continue
		}

		reply, err := udprdma.ProcessDiscoveryPacket(buf[:n], udprdma.Service_UDPFS)
		if err != nil {
			s.logger.Warn("failed to process discovery packet", "peer", addr, "err", err)
			continue
		}
		s.dataConn.WriteToUDP(reply, addr)
		s.logger.Info("discovery request received", "peer", addr)
	}
}
