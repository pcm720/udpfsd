// Session holds UDPRDMA send state and implements reliable send with flow control.
// See docs/UDPRDMA.md.
package udprdma

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// Session is a UDPRDMA data connection session (reliable send/recv state).
type Session struct {
	// Session statistics
	creationTime time.Time
	writeTo      func(addr *net.UDPAddr, data []byte)
	peerAddr     *net.UDPAddr

	pendingSend   *pendingSend
	resetCallback func()
	// Transmit ring buffer
	txBuffer     [2048]txPacket
	txWriteIndex int
	txReadIndex  int

	metricContainer

	sync.Mutex

	txSeqNr       uint16
	txSeqNrAcked  uint16
	rxSeqExpected uint16

	packetBuf [1500]byte

	finAckTicker       *time.Ticker
	retransmitAttempts int
	closeChan          chan struct{}
}

type txPacket struct {
	data []byte
	seq  uint16
}

// pendingSend holds state for a multi-packet send waiting for window ACK.
type pendingSend struct {
	data     []byte
	offset   int
	maxChunk int
}

// NewSession creates a session that sends via writeTo.
func NewSession(peerAddr net.UDPAddr, writeTo func(addr *net.UDPAddr, data []byte)) *Session {
	s := &Session{
		writeTo:      writeTo,
		peerAddr:     &peerAddr,
		creationTime: time.Now(),
		pendingSend:  &pendingSend{},
		finAckTicker: time.NewTicker(time.Minute),
		closeChan:    make(chan struct{}),
	}
	s.finAckTicker.Stop()
	for i := range s.txBuffer {
		s.txBuffer[i] = txPacket{
			data: make([]byte, 1500),
		}
	}
	go s.finAckTracker()

	return s
}

func (s *Session) Close() {
	close(s.closeChan)
}

// Sets function that will be called on peer reset
func (s *Session) SetResetCallback(f func()) {
	s.resetCallback = f
}

// Validates UDPRDMA DATA packet and returns payload to pass to the underlying protocol header or nil otherwise
func (s *Session) ProcessDataPacket(data []byte) (payload []byte, err error) {
	s.Lock()
	defer s.Unlock()

	hdr, err := UnpackHeader(data)
	if err != nil || hdr.PacketType != PacketData {
		return nil, fmt.Errorf("invalid header: %v", err)
	}
	header, err := UnpackDataHeader(data[2:6])
	if err != nil {
		return nil, fmt.Errorf("invalid data header: %v", err)
	}

	s.packetsRx++

	payload = data[6:]
	hdrSize := int(header.HdrWordCount) * 4
	payloadSize := hdrSize + int(header.DataByteCount)
	if payloadSize > len(payload) {
		payloadSize = len(payload)
	}

	if header.Flags&uint8(DataFlagACK) != 0 {
		s.OnAck(header.SeqNrAck)
	}

	if (payloadSize == 0) && (header.Flags&uint8(DataFlagACK) != 0) {
		s.ContinuePendingSend()
		return nil, nil
	}
	if (payloadSize == 0) && (header.Flags&uint8(DataFlagACK) == 0) {
		// On NACK, roll back sequence number and retransmit the previous packet
		s.txSeqNrAcked = (header.SeqNrAck - 1) & 0xFFF
		s.RetransmitFrom(header.SeqNrAck)
		s.peerNACKs++
		return nil, nil
	}

	if hdr.SeqNr != s.rxSeqExpected {
		prevSeq := (s.rxSeqExpected - 1) & 0xFFF
		if hdr.SeqNr == prevSeq {
			s.unexpectedSeqNrs++
			s.sendACK(true)
			log.Printf("[%s]: got previous packet %d (expected %d), acking", s.peerAddr, hdr.SeqNr, s.rxSeqExpected)
			if s.pendingSend.data != nil {
				// Retransmit from the last unacked packet
				s.RetransmitFrom((s.txSeqNrAcked + 1) & 0xFFF)
			}
			return nil, nil
		}
		if hdr.SeqNr == 0 {
			log.Printf("[%s]: got unexpected sequence number 0, assuming the peer was reset", s.peerAddr)
			s.ResetSession()
		} else {
			s.unexpectedSeqNrs++
			log.Printf("[%s]: got unexpected sequence number %d (expected %d)", s.peerAddr, hdr.SeqNr, s.rxSeqExpected)
			s.sendACK(false)
			return nil, nil
		}
	}

	// Update expected RX number and ACK the packet
	s.rxSeqExpected = (hdr.SeqNr + 1) & 0xFFF
	s.sendACK(true)

	return payload[:payloadSize], nil
}

// SendData sends a single DATA packet with full payload and FIN.
func (s *Session) SendData(payload []byte) {
	s.Lock()
	defer s.Unlock()

	s.SendDataPacket(payload, true, 0)
}

// SendRawDataWithHeader sends header + data with header on first packet; may set pending and return.
func (s *Session) SendRawDataWithHeader(header, data []byte) {
	s.Lock()
	defer s.Unlock()

	s.txReadIndex = 0
	s.txWriteIndex = 0
	s.pendingSend.data = nil

	maxChunk := optimalChunkSize(len(data))
	firstDataMax := maxChunk
	if len(header) < MaxDataPayload {
		if MaxDataPayload-len(header) < firstDataMax {
			firstDataMax = MaxDataPayload - len(header)
		}
	}
	firstChunkSize := firstDataMax
	if firstChunkSize > len(data) {
		firstChunkSize = len(data)
	}

	firstPayload := s.packetBuf[:firstChunkSize+len(header)]
	copy(firstPayload, header)
	copy(firstPayload[len(header):], data[:firstChunkSize])

	fin := firstChunkSize >= len(data)
	s.SendDataPacket(firstPayload, fin, len(header))
	offset := firstChunkSize
	for offset < len(data) {
		if s.InFlight() >= SendWindow {
			s.pendingSend.data = data
			s.pendingSend.offset = offset
			s.pendingSend.maxChunk = maxChunk
			return
		}
		chunkSize := maxChunk
		if offset+chunkSize > len(data) {
			chunkSize = len(data) - offset
		}
		fin = offset+chunkSize >= len(data)
		s.SendDataPacket(data[offset:offset+chunkSize], fin, 0)
		offset += chunkSize
	}
}

// SendACK sends an ACK or NACK packet (no payload).
func (s *Session) SendACK(ack bool) {
	s.Lock()
	defer s.Unlock()
	s.sendACK(ack)
}
