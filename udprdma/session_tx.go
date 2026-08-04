package udprdma

import (
	"log"
	"math"
)

// SendData sends a single DATA packet with full payload and FIN
func (s *Session) SendData(payload []byte) {
	s.Lock()
	s.resetSendState()
	s.transfer.header = nil
	s.transfer.data = payload
	s.transfer.offset = 0
	s.transfer.maxChunk = len(payload)
	s.handleTransfer()
	s.Unlock()
}

// SendRawDataWithHeader sends header + data with header on first packet
func (s *Session) SendRawDataWithHeader(header, data []byte) {
	s.Lock()
	s.resetSendState()
	s.transfer.header = header
	s.transfer.data = data
	s.transfer.offset = 0
	s.transfer.maxChunk = optimalChunkSize(len(data))
	s.handleTransfer()
	s.Unlock()
}

// SendACK sends an ACK or NACK packet (no payload)
func (s *Session) SendACK(ack bool) {
	s.Lock()
	defer s.Unlock()
	if ack {
		s.sendACK(true)
	} else {
		s.sendACK(false)
	}
}

// handleTransfer sends more chunks while flow control allows.
// Packets are batched into one writeBatch call.
// Assumes the lock is acquired
func (s *Session) handleTransfer() {
	batch := make([][]byte, 0, SendWindow)
	for s.transfer.data != nil && s.InFlight() < SendWindow {
		// First packet may carry header only (e.g. RESULT_REPLY with 0 data bytes).
		if s.transfer.offset == 0 && len(s.transfer.header) > 0 {
			maxChunk := s.transfer.maxChunk
			firstDataMax := maxChunk
			if len(s.transfer.header) < MaxDataPayload {
				if MaxDataPayload-len(s.transfer.header) < firstDataMax {
					firstDataMax = MaxDataPayload - len(s.transfer.header)
				}
			}
			chunkSize := firstDataMax
			if chunkSize > len(s.transfer.data) {
				chunkSize = len(s.transfer.data)
			}
			fin := chunkSize >= len(s.transfer.data)
			batch = append(batch, s.packDataPacket(s.transfer.header, s.transfer.data[:chunkSize], fin))
			s.transfer.offset = chunkSize
			if fin {
				s.clearTransfer()
			}
			continue
		}

		if s.transfer.offset >= len(s.transfer.data) {
			s.clearTransfer()
			break
		}

		chunkSize := s.transfer.maxChunk
		if s.transfer.offset+chunkSize > len(s.transfer.data) {
			chunkSize = len(s.transfer.data) - s.transfer.offset
		}
		fin := s.transfer.offset+chunkSize >= len(s.transfer.data)
		chunk := s.transfer.data[s.transfer.offset : s.transfer.offset+chunkSize]
		s.transfer.offset += chunkSize
		if fin {
			s.clearTransfer()
		}
		batch = append(batch, s.packDataPacket(nil, chunk, fin))
	}
	if len(batch) > 0 {
		s.writeBatch(s.peerAddr, batch)
	}
	s.updateAckTimer()
}

// handleAckTimeout processes FIN or window ACK timeouts.
// Assumes the lock is acquired
func (s *Session) handleAckTimeout() {
	waitingFin := s.finPending
	waitingWindow := !s.finPending && s.transfer.data != nil && s.InFlight() >= SendWindow
	if !waitingFin && !waitingWindow {
		s.stopAckTimer()
		return
	}

	s.retransmitAttempts++
	from := (s.txSeqNrAcked + 1) & 0xFFF
	sent := s.retransmitFrom(from)
	if sent == 0 {
		log.Printf("[%s]: ACK retransmit from %d sent 0 packets", s.peerAddr, from)
	}

	if s.retransmitAttempts < MaxRetransmits {
		if waitingFin {
			log.Printf("[%s]: FIN ACK timeout for packet %d, retransmitting from %d", s.peerAddr, (s.txSeqNr-1)&0xFFF, from)
		} else {
			log.Printf("[%s]: window ACK timeout, retransmitting from %d", s.peerAddr, from)
		}
		s.armAckTimer()
		return
	}

	if waitingFin {
		log.Printf("[%s]: final FIN ACK timeout for packet %d, retransmitting from %d and giving up", s.peerAddr, (s.txSeqNr-1)&0xFFF, from)
		s.finPending = false
	} else {
		log.Printf("[%s]: final window ACK timeout, aborting transfer from %d", s.peerAddr, from)
		s.clearTransfer()
	}
	s.stopAckTimer()
	s.retransmitAttempts = 0
}

// sendACK sends an ACK or NACK packet (no payload).
// Assumes the lock is acquired
func (s *Session) sendACK(ack bool) {
	flags := uint8(0)
	if ack {
		flags = uint8(DataFlagACK)
	}
	seqAck := (s.rxSeqExpected - 1) & 0xFFF
	if !ack {
		seqAck = s.rxSeqExpected
	}

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

// pruneAcked discards buffered packets covered by seqNrAck.
// Assumes the lock is acquired
func (s *Session) pruneAcked(seqNrAck uint16) {
	for s.txReadIndex != s.txWriteIndex {
		p := s.txBuffer[s.txReadIndex]
		// Keep packets strictly after seqNrAck (half-sequence distance).
		if ((p.seq - seqNrAck - 1) & 0xFFF) < 2048 {
			break
		}
		s.txReadIndex = (s.txReadIndex + 1) % len(s.txBuffer)
	}
}

// retransmitFrom retransmits buffered packets from fromSeq.
// Assumes the lock is acquired. Returns the number of packets sent
func (s *Session) retransmitFrom(fromSeq uint16) int {
	batch := make([][]byte, 0, SendWindow)
	for i := s.txReadIndex; i != s.txWriteIndex; i = (i + 1) % len(s.txBuffer) {
		p := s.txBuffer[i]
		diff := (p.seq - fromSeq) & 0xFFF

		if diff >= 2048 {
			break
		}

		if p.seq != (fromSeq-1)&0xFFF {
			s.packetsTx++
			s.retransmits++
			batch = append(batch, p.data)
		}
	}
	if len(batch) > 0 {
		s.writeBatch(s.peerAddr, batch)
	}
	return len(batch)
}

// packDataPacket packs one DATA packet into the retransmit ring and advances send state.
// Assumes the lock is acquired
func (s *Session) packDataPacket(header, data []byte, fin bool) []byte {
	hdrSize := len(header)
	dataSize := len(data)
	padded := (dataSize + 3) & ^3

	flags := uint8(DataFlagACK)
	if fin {
		flags |= uint8(DataFlagFIN)
	}

	pkt := s.txBuffer[s.txWriteIndex].data[:hdrSize+padded+headerSize+dataHeaderSize]

	Header{PacketType: PacketData, SeqNr: s.txSeqNr}.Pack(pkt)
	DataHeader{
		SeqNrAck: (s.rxSeqExpected - 1) & 0xFFF, Flags: flags,
		HdrWordCount: uint8(hdrSize / 4), DataByteCount: uint16(padded),
	}.Pack(pkt[headerSize:])
	off := headerSize + dataHeaderSize
	copy(pkt[off:], header)
	copy(pkt[off+hdrSize:], data)
	if padded != dataSize {
		clear(pkt[off+hdrSize+dataSize : off+hdrSize+padded])
	}

	s.txBuffer[s.txWriteIndex].seq = s.txSeqNr
	s.txBuffer[s.txWriteIndex].data = pkt
	s.txWriteIndex = (s.txWriteIndex + 1) % len(s.txBuffer)

	if s.txWriteIndex == s.txReadIndex {
		panic("udprdma: ring buffer is full")
	}

	s.txSeqNr = (s.txSeqNr + 1) & 0xFFF
	s.packetsTx++

	if fin {
		s.finPending = true
		s.retransmitAttempts = 0
	}
	return pkt
}

// Calculates the optimal transfer chunk size for totalBytes
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
