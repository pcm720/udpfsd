package udpfs

import (
	"encoding/binary"
	"log"
	"net"
	"os"
	"time"
)

const (
	Flag_ReadOnly  = 0x01
	Flag_WriteOnly = 0x02
	Flag_ReadWrite = 0x03
	Flag_Append    = 0x0100
	Flag_Create    = 0x0200
	Flag_Truncate  = 0x0400
)

// StatInfo is the PS2-compatible stat structure sent in replies.
type StatInfo struct {
	Mode   uint32
	Attr   uint32
	Size   uint32
	Hisize uint32
	Ctime  [8]byte
	Atime  [8]byte
	Mtime  [8]byte
}

// EncodeTime converts Unix time to PS2 iox_stat_t time (8 bytes).
func EncodeTime(t time.Time) [8]byte {
	var out [8]byte
	out[0] = 0
	out[1] = byte(t.Second())
	out[2] = byte(t.Minute())
	out[3] = byte(t.Hour())
	out[4] = byte(t.Day())
	out[5] = byte(t.Month())
	out[6] = byte(t.Year() & 0xFF)
	out[7] = byte(t.Year() >> 8)
	return out
}

// StatInfoFromFile builds StatInfo from os.FileInfo.
func StatInfoFromFile(fi os.FileInfo) StatInfo {
	var st StatInfo
	if fi.Mode().IsRegular() {
		st.Mode = FIO_S_IFREG
	} else if fi.Mode().IsDir() {
		st.Mode = FIO_S_IFDIR
	}
	size := uint64(fi.Size())
	st.Size = uint32(size & 0xFFFFFFFF)
	st.Hisize = uint32(size >> 32)
	t := fi.ModTime()
	st.Ctime = EncodeTime(t)
	st.Atime = EncodeTime(t)
	st.Mtime = EncodeTime(t)
	return st
}

// PackStat appends StatInfo to a buffer (ctime, atime, mtime then optional mode/attr/size/hisize).
func (s StatInfo) PackWithHeader(mode, attr, size, hisize uint32) []byte {
	b := make([]byte, 0, 8+8+8)
	b = append(b, s.Ctime[:]...)
	b = append(b, s.Atime[:]...)
	b = append(b, s.Mtime[:]...)
	return b
}

// PackOpenReply builds OPEN_REPLY payload (msg_type, 3 reserved bytes, handle, mode, size, hisize, ctime[8], mtime[8]).
func PackOpenReply(handle int32, st StatInfo) []byte {
	b := make([]byte, 4+4+4+4+4+4+4+8+8)
	b[0] = byte(MsgOpenReply)
	binary.LittleEndian.PutUint32(b[4:], uint32(handle))
	binary.LittleEndian.PutUint32(b[8:], st.Mode)
	binary.LittleEndian.PutUint32(b[12:], st.Size)
	binary.LittleEndian.PutUint32(b[16:], st.Hisize)
	copy(b[20:], st.Ctime[:])
	copy(b[28:], st.Mtime[:])
	return b
}

// PackCloseReply builds CLOSE_REPLY payload.
func PackCloseReply(result int32) []byte {
	b := make([]byte, 8)
	b[0] = byte(MsgCloseReply)
	binary.LittleEndian.PutUint32(b[4:], uint32(result))
	return b
}

// PackResultReply builds RESULT_REPLY payload (for READ response header).
func PackResultReply(result int32) []byte {
	b := make([]byte, 8)
	b[0] = byte(MsgResultReply)
	binary.LittleEndian.PutUint32(b[4:], uint32(result))
	return b
}

// PackWriteDone builds WRITE_DONE payload.
func PackWriteDone(result int32) []byte {
	b := make([]byte, 8)
	b[0] = byte(MsgWriteDone)
	binary.LittleEndian.PutUint32(b[4:], uint32(result))
	return b
}

// PackLseekReply builds LSEEK_REPLY payload.
func PackLseekReply(position int64) []byte {
	b := make([]byte, 12)
	b[0] = byte(MsgLseekReply)
	if position < 0 {
		binary.LittleEndian.PutUint32(b[4:], uint32(position))
		binary.LittleEndian.PutUint32(b[8:], 0xFFFFFFFF)
	} else {
		binary.LittleEndian.PutUint32(b[4:], uint32(position&0xFFFFFFFF))
		binary.LittleEndian.PutUint32(b[8:], uint32(position>>32))
	}
	return b
}

// PackDreadReply builds DREAD_REPLY payload
func PackDreadReply(result int32, name string, st StatInfo) []byte {
	nameBytes := []byte(name)
	if len(nameBytes) > 0 {
		nameBytes = append(nameBytes, 0)
	}
	nameLen := len(nameBytes)
	if nameLen > 0 {
		nameLen--
	}
	paddedNameLen := (len(nameBytes) + 3) & ^3
	for len(nameBytes) < paddedNameLen {
		nameBytes = append(nameBytes, 0)
	}
	// Fixed part: msg_type(1) + reserved(1) + name_len(2) + result(4) + mode(4) + attr(4) + size(4) + hisize(4) + ctime(8) + atime(8) + mtime(8) = 48
	b := make([]byte, 48)
	b[0] = byte(MsgDreadReply)
	b[1] = 0 // reserved
	binary.LittleEndian.PutUint16(b[2:], uint16(nameLen))
	binary.LittleEndian.PutUint32(b[4:], uint32(result))
	binary.LittleEndian.PutUint32(b[8:], st.Mode)
	binary.LittleEndian.PutUint32(b[12:], st.Attr)
	binary.LittleEndian.PutUint32(b[16:], st.Size)
	binary.LittleEndian.PutUint32(b[20:], st.Hisize)
	copy(b[24:], st.Ctime[:])
	copy(b[32:], st.Atime[:])
	copy(b[40:], st.Mtime[:])
	if result > 0 {
		b = append(b, nameBytes...)
	}
	return b
}

// PackGetstatReply builds GETSTAT_REPLY payload
func PackGetstatReply(result int32, st StatInfo) []byte {
	b := make([]byte, 48)
	b[0] = byte(MsgGetstatReply)
	// b[1:4] reserved
	binary.LittleEndian.PutUint32(b[4:], uint32(result))
	binary.LittleEndian.PutUint32(b[8:], st.Mode)
	binary.LittleEndian.PutUint32(b[12:], st.Attr)
	binary.LittleEndian.PutUint32(b[16:], st.Size)
	binary.LittleEndian.PutUint32(b[20:], st.Hisize)
	copy(b[24:], st.Ctime[:])
	copy(b[32:], st.Atime[:])
	copy(b[40:], st.Mtime[:])
	return b
}

func logPayload(addr *net.UDPAddr, msgType MsgType, status int, payload []byte) {
	switch msgType {
	case MsgOpenReq:
		pathEnd := 0
		for pathEnd < len(payload)-8 && payload[8+pathEnd] != 0 {
			pathEnd++
		}
		path := string(payload[8 : 8+pathEnd])
		isDir := len(payload) > 1 && payload[1] != 0
		if isDir {
			log.Printf("[%s]: DOPEN %q: %d", addr, path, status)
		} else {
			log.Printf("[%s]: OPEN %q: %d", addr, path, status)
		}
	case MsgCloseReq:
		if len(payload) >= 8 {
			handle := int32(uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24)
			log.Printf("[%s]: CLOSE handle=%d: %d", addr, handle, status)
		}
	case MsgReadReq:
		if len(payload) >= 12 {
			handle := int32(uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24)
			size := uint32(payload[8]) | uint32(payload[9])<<8 | uint32(payload[10])<<16 | uint32(payload[11])<<24
			log.Printf("[%s]: READ handle=%d size=%d: %d", addr, handle, size, status)
		}
	case MsgWriteReq:
		if len(payload) >= 12 {
			handle := int32(uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24)
			size := uint32(payload[8]) | uint32(payload[9])<<8 | uint32(payload[10])<<16 | uint32(payload[11])<<24
			log.Printf("[%s]: WRITE handle=%d size=%d: %d", addr, handle, size, status)
		}
	case MsgWriteData:
		if len(payload) >= 8 {
			chunkNr := int32(uint32(payload[2]) | uint32(payload[3])<<8)
			chunkSize := int32(uint32(payload[4]) | uint32(payload[5])<<8)
			totalChunks := int32(uint32(payload[6]) | uint32(payload[7])<<8)
			log.Printf("[%s]: WRITE_DATA chunk=%d size=%d total=%d: %d", addr, chunkNr, chunkSize, totalChunks, status)
		}
	case MsgWriteDone:
		if len(payload) >= 8 {
			result := int32(uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24)
			log.Printf("[%s]: WRITE_DONE result=%d: %d", addr, result, status)
		}
	case MsgLseekReq:
		if len(payload) >= 16 {
			handle := int32(uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24)
			log.Printf("[%s]: LSEEK handle=%d: %d", addr, handle, status)
		}
	case MsgDreadReq:
		if len(payload) >= 8 {
			handle := int32(uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24)
			log.Printf("[%s]: DREAD handle=%d: %d", addr, handle, status)
		}
	case MsgGetstatReq:
		pathEnd := 0
		for pathEnd < len(payload)-4 && payload[4+pathEnd] != 0 {
			pathEnd++
		}
		path := string(payload[4 : 4+pathEnd])
		log.Printf("[%s]: GETSTAT %q: %d", addr, path, status)
	case MsgMkdirReq:
		pathEnd := 0
		for pathEnd < len(payload)-4 && payload[4+pathEnd] != 0 {
			pathEnd++
		}
		log.Printf("[%s]: MKDIR %q: %d", addr, string(payload[4:4+pathEnd]), status)
	case MsgRemoveReq:
		pathEnd := 0
		for pathEnd < len(payload)-4 && payload[4+pathEnd] != 0 {
			pathEnd++
		}
		log.Printf("[%s]: REMOVE %q: %d", addr, string(payload[4:4+pathEnd]), status)
	case MsgRmdirReq:
		pathEnd := 0
		for pathEnd < len(payload)-4 && payload[4+pathEnd] != 0 {
			pathEnd++
		}
		log.Printf("[%s]: RMDIR %q: %d", addr, string(payload[4:4+pathEnd]), status)
	case MsgBreadReq:
		if len(payload) >= 16 {
			handle := int32(uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24)
			sectorCount := uint16(payload[2]) | uint16(payload[3])<<8
			sectorNrLo := binary.LittleEndian.Uint32(payload[8:12])
			sectorNrHi := binary.LittleEndian.Uint32(payload[12:16])
			sectorNr := int64(sectorNrHi)<<32 | int64(sectorNrLo)
			log.Printf("[%s]: BREAD handle=%d sector=%d sector_count=%d: %d", addr, handle, sectorNr, sectorCount, status)
		}
	case MsgBwriteReq:
		if len(payload) >= 16 {
			handle := int32(uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24)
			sectorCount := uint16(payload[2]) | uint16(payload[3])<<8
			sectorNrLo := binary.LittleEndian.Uint32(payload[8:12])
			sectorNrHi := binary.LittleEndian.Uint32(payload[12:16])
			sectorNr := int64(sectorNrHi)<<32 | int64(sectorNrLo)
			log.Printf("[%s]: BWRITE handle=%d sector=%d sector_count=%d: %d", addr, handle, sectorNr, sectorCount, status)
		}
	}
}
