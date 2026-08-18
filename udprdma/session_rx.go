package udprdma

import "fmt"

// ProcessDataPacket validates a UDPRDMA DATA packet and returns payload for the
// underlying protocol, or nil otherwise
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

	isAck := header.Flags&uint8(DataFlagACK) != 0

	// Control packets (empty payload): peer ACK or NACK only.
	if payloadSize == 0 {
		seq := header.SeqNrAck
		if isAck {
			s.onPeerAck(seq)
			return nil, nil
		}
		s.peerNACKs++
		s.onPeerNack(seq)
		return nil, nil
	}

	// Handle out-of-sequence packets
	if hdr.SeqNr != s.rxSeqExpected {
		prevSeq := (s.rxSeqExpected - 1) & 0xFFF
		if hdr.SeqNr == prevSeq {
			s.unexpectedSeqNrs++
			s.logger.Warn("got previous packet, acking", "peer", s.peerAddr, "seq", hdr.SeqNr, "expected", s.rxSeqExpected)
			retransmit := s.transfer.data != nil
			retransmitFrom := (s.txSeqNrAcked + 1) & 0xFFF
			s.sendACK(true)
			if isAck {
				s.onPeerAck(header.SeqNrAck)
			}
			if retransmit {
				s.onPeerNack(retransmitFrom)
			}
			return nil, nil
		}
		if hdr.SeqNr == 0 {
			s.logger.Warn("got unexpected sequence number 0, assuming the peer was reset", "peer", s.peerAddr)
			s.ResetSession()
		} else {
			s.unexpectedSeqNrs++
			s.logger.Warn("got unexpected sequence number", "peer", s.peerAddr, "seq", hdr.SeqNr, "expected", s.rxSeqExpected)
			s.sendACK(false)
			if isAck {
				s.onPeerAck(header.SeqNrAck)
			}
			return nil, nil
		}
	}

	s.rxSeqExpected = (hdr.SeqNr + 1) & 0xFFF
	s.sendACK(true)
	if isAck {
		s.onPeerAck(header.SeqNrAck)
	}
	return payload[:payloadSize], nil
}
