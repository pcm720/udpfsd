package udprdma

import (
	"log"
	"math"
)

// ContinuePendingSend sends more chunks when flow control allows (call after OnAck).
func (s *Session) ContinuePendingSend() {
	for s.pendingSend.data != nil && s.InFlight() < SendWindow {
		ps := s.pendingSend
		if ps.offset >= len(ps.data) {
			s.pendingSend.data = nil
			return
		}
		chunkSize := ps.maxChunk
		if ps.offset+chunkSize > len(ps.data) {
			chunkSize = len(ps.data) - ps.offset
		}
		fin := ps.offset+chunkSize >= len(ps.data)
		chunk := ps.data[ps.offset : ps.offset+chunkSize]
		ps.offset += chunkSize
		if ps.offset >= len(ps.data) {
			s.pendingSend.data = nil
		}
		s.SendDataPacket(chunk, fin, 0)
	}
}

func (s *Session) finAckTracker() {
	for {
		select {
		case <-s.finAckTicker.C:
			s.Lock()
			s.retransmitAttempts++
			s.RetransmitFrom((s.txSeqNrAcked + 1) & 0xFFF)
			if s.retransmitAttempts == MaxRetransmits {
				// Max number of retransmits exceeded, give up
				log.Printf("[%s]: final FIN ACK timeout for packet %d, retransmitting from %d and giving up", s.peerAddr, (s.txSeqNr-1)&0xFFF, (s.txSeqNrAcked+1)&0xFFF)
				s.finAckTicker.Stop()
			} else {
				log.Printf("[%s]: FIN ACK timeout for packet %d, retransmitting from %d", s.peerAddr, (s.txSeqNr-1)&0xFFF, (s.txSeqNrAcked+1)&0xFFF)
			}
			s.Unlock()
		case <-s.closeChan:
			s.finAckTicker.Stop()
			return
		}
	}
}

// SendDataPacket sends one DATA packet and buffers it for retransmit.
func (s *Session) SendDataPacket(payload []byte, fin bool, hdrSize int) {
	dataSize := len(payload) - hdrSize
	padded := (dataSize + 3) & ^3

	flags := uint8(DataFlagACK)
	if fin {
		flags |= uint8(DataFlagFIN)
	}

	// Set len to required size
	pkt := s.txBuffer[s.txWriteIndex].data[:hdrSize+padded+headerSize+dataHeaderSize]

	// Assemble the packet
	Header{PacketType: PacketData, SeqNr: s.txSeqNr}.Pack(pkt)
	DataHeader{
		SeqNrAck: (s.rxSeqExpected - 1) & 0xFFF, Flags: flags,
		HdrWordCount: uint8(hdrSize / 4), DataByteCount: uint16(padded),
	}.Pack(pkt[headerSize:])
	copy(pkt[headerSize+dataHeaderSize:], payload)
	// Zero-out padding
	clear(pkt[headerSize+dataHeaderSize+len(payload):])

	s.txBuffer[s.txWriteIndex].seq = s.txSeqNr
	s.txBuffer[s.txWriteIndex].data = pkt
	s.txWriteIndex = (s.txWriteIndex + 1) % len(s.txBuffer)

	if s.txWriteIndex == s.txReadIndex {
		panic("udprdma: ring buffer is full")
	}

	s.txSeqNr = (s.txSeqNr + 1) & 0xFFF
	s.writeTo(s.peerAddr, pkt)
	s.packetsTx++

	if fin {
		// FIN packet sent, reset the tracker timer to start FIN ACK window
		s.retransmitAttempts = 0
		s.finAckTicker.Reset(RetransmitTimeout)
	}
}

// OnAck updates send state from a received ACK and prunes txBuffer.
func (s *Session) OnAck(seqNrAck uint16) {
	s.txSeqNrAcked = seqNrAck

	if seqNrAck == ((s.txSeqNr - 1) & 0xFFF) {
		// This ACK confirms a FIN packet, stop ACK tracking
		s.finAckTicker.Stop()
		s.retransmitAttempts = 0
	}

	// Move read index forward, skipping all acked packets
	for s.txReadIndex != s.txWriteIndex {
		p := s.txBuffer[s.txReadIndex]
		// Packet can be discarded (slot will be reused)
		s.txReadIndex = (s.txReadIndex + 1) % len(s.txBuffer)
		if p.seq == seqNrAck {
			// Last acked packet in the buffer, stop
			break
		}
	}
}

// RetransmitFrom retransmits buffered packets from fromSeq.
func (s *Session) RetransmitFrom(fromSeq uint16) {
	for i := s.txReadIndex; i != s.txWriteIndex; i = (i + 1) % len(s.txBuffer) {
		p := s.txBuffer[i]
		diff := (p.seq - fromSeq) & 0xFFF

		// Check if this packet is in the retransmit window
		if diff >= 2048 {
			break // All remaining packets are outside window
		}

		// Only retransmit if seq >= fromSeq
		if p.seq != (fromSeq-1)&0xFFF && diff < 2048 {
			s.packetsTx++
			s.retransmits++
			s.writeTo(s.peerAddr, p.data)
		}
	}

	// Reset tracking goroutine timer
	s.finAckTicker.Reset(RetransmitTimeout)
}

// sendACK sends an ACK or NACK packet (no payload).
// Internal version that does not lock
func (s *Session) sendACK(ack bool) {
	flags := uint8(0)
	if ack {
		flags = uint8(DataFlagACK)
	}
	seqAck := (s.rxSeqExpected - 1) & 0xFFF
	if !ack {
		seqAck = s.rxSeqExpected
	}

	// Set len to required size and clear
	pkt := s.packetBuf[:headerSize+dataHeaderSize]

	Header{PacketType: PacketData, SeqNr: s.txSeqNr}.Pack(pkt)
	DataHeader{
		SeqNrAck: seqAck, Flags: flags, HdrWordCount: 0, DataByteCount: 0,
	}.Pack(pkt[headerSize:])
	s.writeTo(s.peerAddr, pkt)

	s.packetsTx++
	if !ack {
		s.nacks++
	}
}

// ResetSession resets session state (e.g. on peer reset, seq=0).
func (s *Session) ResetSession() {
	s.txSeqNr = 0
	s.txSeqNrAcked = 0
	s.txReadIndex = 0
	s.txWriteIndex = 0
	s.pendingSend.data = nil
	s.rxSeqExpected = 0
	s.finAckTicker.Stop()
	s.retransmitAttempts = 0
	s.peerResets++
	if s.resetCallback != nil {
		s.resetCallback()
	}
}

// InFlight returns the number of unacknowledged packets.
func (s *Session) InFlight() int {
	return int((s.txSeqNr - s.txSeqNrAcked - 1) & 0xFFF)
}

// Returns the best chunk size for given number of bytes
func optimalChunkSize(totalBytes int) int {
	bestChunk := 1408
	bestPackets := int(math.Ceil(float64(totalBytes) / 1408))
	for _, maxChunk := range []int{1024, 1280, 1408} {
		packets := int(math.Ceil(float64(totalBytes) / float64(maxChunk)))
		if packets < bestPackets {
			bestPackets = packets
			bestChunk = maxChunk
		}
	}
	return bestChunk
}
