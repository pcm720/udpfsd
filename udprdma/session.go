// Session holds UDPRDMA send state and implements reliable send with flow control.
// See docs/UDPRDMA.md.
package udprdma

import (
	"net"
	"sync"
	"time"
)

// Session is a UDPRDMA data connection session
type Session struct {
	creationTime time.Time
	writeTo      func(addr *net.UDPAddr, data []byte)
	writeBatch   func(addr *net.UDPAddr, packets [][]byte)
	peerAddr     *net.UDPAddr

	resetCallback func()

	// Transmit ring buffer
	txBuffer     [2048]txPacket
	txWriteIndex int
	txReadIndex  int

	// Single outstanding transfer
	transfer transfer

	metricContainer

	sync.Mutex

	txSeqNr       uint16
	txSeqNrAcked  uint16
	rxSeqExpected uint16

	packetBuf [1500]byte

	// Retransmit state tracking
	ackTimer           *time.Timer
	ackTimerExpectSeq  uint16
	ackTimerSeq        uint16 // Incremented on arm/stop to invalidate stale callbacks
	ackWaitMode        ackWaitMode
	retransmitAttempts int
	finPending         bool // True after FIN sent until it is ACKed or retransmits time out

	closed bool
}

type txPacket struct {
	data []byte
	seq  uint16
}

// transfer holds state for one outbound multi-packet send.
type transfer struct {
	header   []byte
	data     []byte
	offset   int
	maxChunk int
}

// NewSession creates a session that sends via writeTo.
// If writeBatch is nil, writeTo is used to send batches in a loop
func NewSession(peerAddr net.UDPAddr, writeTo func(addr *net.UDPAddr, data []byte), writeBatch func(addr *net.UDPAddr, packets [][]byte)) *Session {
	s := &Session{
		writeTo:      writeTo,
		writeBatch:   writeBatch,
		peerAddr:     &peerAddr,
		creationTime: time.Now(),
		txSeqNrAcked: 0xFFF, // nothing acked yet (−1 mod 4096)
	}
	if s.writeBatch == nil {
		s.writeBatch = func(addr *net.UDPAddr, packets [][]byte) {
			for _, p := range packets {
				writeTo(addr, p)
			}
		}
	}
	// Single reusable timer
	s.ackTimer = time.AfterFunc(time.Hour, s.ackTimerFunc)
	s.ackTimer.Stop()
	for i := range s.txBuffer {
		s.txBuffer[i] = txPacket{
			data: make([]byte, 1500),
		}
	}
	return s
}

// Close stops the TX loop.
func (s *Session) Close() {
	s.Lock()
	if s.closed {
		s.Unlock()
		return
	}
	s.closed = true
	s.stopAckTimer()
	s.Unlock()
}

// SetResetCallback sets function that will be called on peer reset.
func (s *Session) SetResetCallback(f func()) {
	s.resetCallback = f
}

// ResetSession resets session state (e.g. on peer reset, seq=0).
// Assumes the lock is acquired
func (s *Session) ResetSession() {
	s.txSeqNr = 0
	s.txSeqNrAcked = 0xFFF
	s.txReadIndex = 0
	s.txWriteIndex = 0
	s.rxSeqExpected = 0
	s.stopAckTimer()
	s.retransmitAttempts = 0
	s.finPending = false
	s.peerResets++
	s.clearTransfer()
	if s.resetCallback != nil {
		s.resetCallback()
	}
}

// InFlight returns the number of unacknowledged packets.
func (s *Session) InFlight() int {
	if s.txReadIndex == s.txWriteIndex {
		return 0
	}
	return int((s.txSeqNr - s.txSeqNrAcked - 1) & 0xFFF)
}

// seqBetween reports whether seq is in [start, end] on the 12-bit ring.
func seqBetween(start, seq, end uint16) bool {
	start &= 0xFFF
	seq &= 0xFFF
	end &= 0xFFF
	if start <= end {
		return seq >= start && seq <= end
	}
	return seq >= start || seq <= end
}

// Clears the pending transfer
func (s *Session) clearTransfer() {
	s.transfer.header = nil
	s.transfer.data = nil
	s.transfer.offset = -1
}

// resetSendState clears the TX buffer and transfer state.
// Assumes the lock is acquired
func (s *Session) resetSendState() {
	s.txReadIndex = 0
	s.txWriteIndex = 0
	s.stopAckTimer()
	s.retransmitAttempts = 0
	s.finPending = false
}
