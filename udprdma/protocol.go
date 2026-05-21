// Package udprdma implements the UDPRDMA protocol
// See docs/UDPRDMA.md for details
package udprdma

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Protocol constants
const (
	UDPFSPort = 0xF5F6 // Default UDP port
)

// Known services
type ServiceType uint16

const (
	Service_UDPFS ServiceType = 0xF5F5
)

// Flow control
const (
	SendWindow        = 8                      // Max unacked packets in flight
	MaxDataPayload    = 1408                   // Max UDPRDMA data payload per packet
	RetransmitTimeout = 500 * time.Millisecond // Max wait before retransmitting if no ACK/NACK received
	MaxRetransmits    = 4
)

// PacketType is the UDPRDMA packet type (4 bits)
type PacketType uint8

const (
	PacketDiscovery PacketType = 0
	PacketInform    PacketType = 1
	PacketData      PacketType = 2
)

// DataFlags for DATA packets (2 bits)
type DataFlags uint8

const (
	DataFlagACK DataFlags = 1
	DataFlagFIN DataFlags = 2
)

// Header sizes in bytes
const (
	headerSize     = 2 // UDPRDMA base header
	discHeaderSize = 4 // Discovery packet header
	dataHeaderSize = 4 // Data packet header
)

// Header is the 2-byte UDPRDMA base header
type Header struct {
	PacketType PacketType // 4 bits
	SeqNr      uint16     // 12 bits
}

// UnpackHeader reads Header from data (at least 2 bytes)
func UnpackHeader(data []byte) (Header, error) {
	if len(data) < 2 {
		return Header{}, fmt.Errorf("header too short")
	}
	val := binary.LittleEndian.Uint16(data[:2])
	return Header{
		PacketType: PacketType(val & 0xF),
		SeqNr:      (val >> 4) & 0xFFF,
	}, nil
}

// Pack writes the header (2 bytes) to given slice
func (h Header) Pack(b []byte) {
	val := uint16(h.PacketType&0xF) | (uint16(h.SeqNr&0xFFF) << 4)
	binary.LittleEndian.PutUint16(b, val)
}

// DiscHeader is the Discovery/Inform header (4 bytes)
type DiscHeader struct {
	ServiceID uint16
	Reserved  uint16 // Must be 0; client uses UDP source port of INFORM for data connection
}

// UnpackDiscHeader reads DiscHeader from data (at least 4 bytes)
func UnpackDiscHeader(data []byte) (DiscHeader, error) {
	if len(data) < 4 {
		return DiscHeader{}, fmt.Errorf("disc header too short")
	}
	return DiscHeader{
		ServiceID: binary.LittleEndian.Uint16(data[0:2]),
		Reserved:  binary.LittleEndian.Uint16(data[2:4]),
	}, nil
}

// Pack writes the discovery header (4 bytes) to given slice
func (d DiscHeader) Pack(b []byte) {
	binary.LittleEndian.PutUint16(b[0:2], d.ServiceID)
	binary.LittleEndian.PutUint16(b[2:4], d.Reserved)
}

// DataHeader is the DATA packet header (4 bytes)
type DataHeader struct {
	SeqNrAck      uint16 // 12 bits
	Flags         uint8  // 2 bits
	HdrWordCount  uint8  // 4 bits: app header size in 4-byte words
	DataByteCount uint16 // 14 bits
}

// UnpackDataHeader reads DataHeader from data (at least 4 bytes)
func UnpackDataHeader(data []byte) (DataHeader, error) {
	if len(data) < 4 {
		return DataHeader{}, fmt.Errorf("data header too short")
	}
	val := binary.LittleEndian.Uint32(data[:4])
	return DataHeader{
		SeqNrAck:      uint16(val & 0xFFF),
		Flags:         uint8((val >> 12) & 0x3),
		HdrWordCount:  uint8((val >> 14) & 0xF),
		DataByteCount: uint16((val >> 18) & 0x3FFF),
	}, nil
}

// Pack writes the data header (4 bytes) to given slice
func (d DataHeader) Pack(b []byte) {
	val := uint32(d.SeqNrAck&0xFFF) |
		(uint32(d.Flags&0x3) << 12) |
		(uint32(d.HdrWordCount&0xF) << 14) |
		(uint32(d.DataByteCount&0x3FFF) << 18)
	binary.LittleEndian.PutUint32(b, val)
}

// Validates UDPRDMA DISCOVERY packet and returns INFORM reply for the server to respond with
func ProcessDiscoveryPacket(data []byte, expectedService ServiceType) (reply []byte, err error) {
	hdr, err := UnpackHeader(data)
	if err != nil {
		return nil, fmt.Errorf("invalid header: %v", err)
	}
	if hdr.PacketType != PacketDiscovery {
		return nil, fmt.Errorf("wrong packet type %d (expected 0/DISCOVERY)", hdr.PacketType)
	}
	disc, err := UnpackDiscHeader(data[2:])
	if err != nil {
		return nil, fmt.Errorf("invalid disc header: %v", err)
	}
	if disc.ServiceID != uint16(expectedService) {
		return nil, fmt.Errorf("wrong service ID 0x%04X (expected 0x%04X)", disc.ServiceID, expectedService)
	}

	reply = make([]byte, headerSize+discHeaderSize)
	Header{PacketType: PacketInform, SeqNr: 1}.Pack(reply)
	DiscHeader{ServiceID: uint16(expectedService), Reserved: 0}.Pack(reply[headerSize:])
	return reply, nil
}
