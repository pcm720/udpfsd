package udprdma

import "log"

// onAck updates send state from a received ACK and prunes txBuffer.
// Assumes the lock is acquired
func (s *Session) onAck(seqNrAck uint16) {
	if s.txReadIndex == s.txWriteIndex {
		return
	}
	lastSent := (s.txSeqNr - 1) & 0xFFF
	if !seqBetween(s.txSeqNrAcked, seqNrAck, lastSent) {
		return
	}

	s.txSeqNrAcked = seqNrAck
	s.pruneAcked(seqNrAck)
	s.retransmitAttempts = 0

	if seqNrAck == lastSent {
		s.stopAckTimer()
		s.finPending = false
		return
	}

	s.updateAckTimer()
}

// Handles peer ACK.
// Assumes the lock is acquired
func (s *Session) onPeerAck(seq uint16) {
	s.onAck(seq)
	if s.transfer.data != nil {
		s.handleTransfer()
	}
}

// Handles peer NACK.
// Assumes the lock is acquired
func (s *Session) onPeerNack(seq uint16) {
	// Go-Back-N from indicated seq; do not move txSeqNrAcked backwards.
	s.retransmitAttempts = 0
	sent := s.retransmitFrom(seq)
	if sent == 0 {
		log.Printf("[%s]: NACK retransmit from %d sent 0 packets", s.peerAddr, seq)
	}
	s.updateAckTimer()
}
