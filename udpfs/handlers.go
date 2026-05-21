package udpfs

import (
	"bytes"
	"encoding/binary"
	"log"
	"net"
)

// HandlePayload parses the message type and dispatches to the appropriate handler.
// Call this when a UDPFS data payload has been received and session state has been updated.
// All handle functions must return status code, where the code is:
// - negative on error
// - 0 on success
// - >0 on success for handlers that read or write data (must contain the number of bytes read/written)
func (c *Connection) HandlePayload(addr *net.UDPAddr, payload []byte) {
	if len(payload) == 0 {
		return
	}
	msgType := MsgType(payload[0])

	statusCode := 0
	switch msgType {
	case MsgOpenReq:
		statusCode = c.handleOpen(addr, payload)
	case MsgCloseReq:
		statusCode = c.handleClose(addr, payload)
	case MsgReadReq:
		statusCode = c.handleRead(addr, payload)
	case MsgWriteReq:
		statusCode = c.handleWriteReq(addr, payload)
	case MsgWriteData:
		statusCode = c.handleWriteData(addr, payload)
	case MsgLseekReq:
		statusCode = c.handleLseek(addr, payload)
	case MsgDreadReq:
		statusCode = c.handleDread(addr, payload)
	case MsgGetstatReq:
		statusCode = c.handleGetstat(addr, payload)
	case MsgMkdirReq:
		statusCode = c.handleMkdir(addr, payload)
	case MsgRemoveReq:
		statusCode = c.handleRemove(addr, payload)
	case MsgRmdirReq:
		statusCode = c.handleRmdir(addr, payload)
	case MsgBreadReq:
		statusCode = c.handleBread(addr, payload)
	case MsgBwriteReq:
		statusCode = c.handleBwriteReq(addr, payload)
	default:
		log.Printf("[%s]: unknown message type: 0x%02x", addr, msgType)
		c.SendACK(addr, true)
		return
	}
	if c.verbose {
		logPayload(addr, msgType, statusCode, payload)
	}
	if c.metricCollector != nil {
		c.logMetric(msgType, statusCode)
	}
}

// handleOpen parses OPEN_REQ payload, calls fs.Open, sends OPEN_REPLY.
func (c *Connection) handleOpen(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 8 {
		c.SendOpenReply(addr, -EINVAL, StatInfo{})
		return -EINVAL
	}
	flags := binary.LittleEndian.Uint16(payload[2:4])
	pathEnd := bytes.IndexByte(payload[8:], 0)
	if pathEnd < 0 {
		pathEnd = len(payload) - 8
	}
	path := string(payload[8 : 8+pathEnd])
	isDir := len(payload) > 1 && payload[1] != 0
	flag := udpfsFlagsToOSFlags(flags)

	if !isDir {
		// Retrieve cached file handle if peer was reset and tries to open the same file again
		if h, ok := c.lookupHandle(path, flag); ok {
			if c.verbose {
				log.Printf("[%s]: reusing file handle %d for %s (mode %x)", addr, h, path, flag)
			}
			c.SendOpenReply(addr, h, StatInfo{})
			return 0
		}
	}

	// Else, open the file
	handle, stat, err := c.fs.Open(path, flag, isDir)
	if err != nil {
		errCode := -errToErrno(err)
		c.SendOpenReply(addr, errCode, stat)
		return int(errCode)
	}
	if handle < 0 {
		c.SendOpenReply(addr, handle, stat)
		return int(handle)
	}

	c.addHandle(handle, path, flag, isDir)

	c.SendOpenReply(addr, handle, stat)
	return 0
}

// handleClose parses CLOSE_REQ, calls fs.Close, sends CLOSE_REPLY.
func (c *Connection) handleClose(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 8 {
		c.SendCloseReply(addr, -EINVAL)
		return -EINVAL
	}
	handle := int32(binary.LittleEndian.Uint32(payload[4:8]))

	if handle == BlockDeviceHandle {
		c.SendCloseReply(addr, handle)
		return int(handle)
	}
	if err := c.fs.Close(handle); err != nil {
		errCode := -errToErrno(err)
		c.SendCloseReply(addr, errCode)
		return int(errCode)
	}

	c.removeHandle(handle)

	c.SendCloseReply(addr, 0)
	return 0
}

// handleRead parses READ_REQ, calls fs.Read, sends RESULT_REPLY + data.
func (c *Connection) handleRead(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 12 {
		c.SendReadResult(addr, -EINVAL, nil)
		return -EINVAL
	}
	handle := int32(binary.LittleEndian.Uint32(payload[4:8]))
	size := binary.LittleEndian.Uint32(payload[8:12])

	c.Lock()
	defer c.Unlock()
	n, data, err := c.fs.Read(handle, size, c.dataBuffer[:cap(c.dataBuffer)])
	if err != nil {
		errCode := -errToErrno(err)
		c.SendReadResult(addr, errCode, nil)
		return int(errCode)
	}
	c.SendReadResult(addr, n, data)
	return int(n)
}

// handleWriteReq parses WRITE_REQ, calls fs.WriteStart; if first chunk in payload, parses and calls WriteChunk; if done, calls fs.CompleteWrite and sends WRITE_DONE.
func (c *Connection) handleWriteReq(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 12 {
		c.SendACK(addr, true)
		return 0
	}
	handle := int32(binary.LittleEndian.Uint32(payload[4:8]))

	if err := c.fs.WriteStart(handle); err != nil {
		errCode := -errToErrno(err)
		c.SendWriteDone(addr, errCode)
		return int(errCode)
	}
	c.setWriteHandle(handle)

	if len(payload) <= 12 {
		c.SendACK(addr, true)
		return 0
	}
	// First chunk in same packet: bytes 12+ are chunk (reserved(2), chunkNr(2), chunkSize(2), totalChunks(2), data)
	chunkPayload := payload[12:]
	if len(chunkPayload) < 8 {
		c.SendACK(addr, true)
		return 0
	}
	chunkNr := binary.LittleEndian.Uint16(chunkPayload[2:4])
	chunkSize := binary.LittleEndian.Uint16(chunkPayload[4:6])
	totalChunks := binary.LittleEndian.Uint16(chunkPayload[6:8])
	chunkData := chunkPayload[8:]
	if int(chunkSize) < len(chunkData) {
		chunkData = chunkData[:chunkSize]
	}
	done, err := c.fs.WriteChunk(handle, chunkNr, chunkSize, totalChunks, chunkData)
	if err != nil {
		errCode := -errToErrno(err)
		c.SendWriteDone(addr, errCode)
		return int(errCode)
	}
	if done {
		n, err := c.fs.CompleteWrite(handle)
		if err != nil {
			errCode := -errToErrno(err)
			c.SendWriteDone(addr, errCode)
			return int(errCode)
		}
		c.SendWriteDone(addr, n)
		return len(payload)
	}
	c.SendACK(addr, true)
	return len(payload)
}

// handleWriteData parses WRITE_DATA chunk, calls fs.WriteChunk; if done, calls fs.CompleteWrite and sends WRITE_DONE.
func (c *Connection) handleWriteData(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 8 {
		c.SendACK(addr, true)
		return 0
	}
	handle := c.getWriteHandle()
	if handle == -1 {
		c.SendACK(addr, true)
		return EBADF
	}

	chunkNr := binary.LittleEndian.Uint16(payload[2:4])
	chunkSize := binary.LittleEndian.Uint16(payload[4:6])
	totalChunks := binary.LittleEndian.Uint16(payload[6:8])
	chunkData := payload[8:]
	if int(chunkSize) < len(chunkData) {
		chunkData = chunkData[:chunkSize]
	}

	done, err := c.fs.WriteChunk(handle, chunkNr, chunkSize, totalChunks, chunkData)
	if err != nil {
		errCode := -errToErrno(err)
		c.SendWriteDone(addr, errCode)
		return int(errCode)
	}
	if done {
		n, err := c.fs.CompleteWrite(handle)
		if err != nil {
			errCode := -errToErrno(err)
			c.SendWriteDone(addr, errCode)
			return int(errCode)
		}
		c.SendWriteDone(addr, n)
		return len(payload)
	}
	c.SendACK(addr, true)
	return len(payload)
}

// handleLseek parses LSEEK_REQ, calls fs.Lseek, sends LSEEK_REPLY.
func (c *Connection) handleLseek(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 16 {
		c.SendLseekReply(addr, -1)
		return -1
	}
	handle := int32(binary.LittleEndian.Uint32(payload[4:8]))
	offsetLo := binary.LittleEndian.Uint32(payload[8:12])
	offsetHi := binary.LittleEndian.Uint32(payload[12:16])
	offset := int64(offsetHi)<<32 | int64(offsetLo)
	whence := int(payload[1])

	pos, err := c.fs.Lseek(handle, offset, whence)
	if err != nil {
		c.SendLseekReply(addr, -1)
		return -1
	}
	c.SendLseekReply(addr, pos)
	return 0
}

// handleDread parses DREAD_REQ, calls fs.Dread, sends DREAD_REPLY.
func (c *Connection) handleDread(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 8 {
		c.SendDreadReply(addr, -EINVAL, "", StatInfo{})
		return -EINVAL
	}
	handle := int32(binary.LittleEndian.Uint32(payload[4:8]))

	ok, name, stat, err := c.fs.Dread(handle)
	if err != nil {
		errCode := -errToErrno(err)
		c.SendDreadReply(addr, errCode, "", StatInfo{})
		return int(errCode)
	}
	if !ok {
		c.SendDreadReply(addr, 0, "", StatInfo{})
		return 0
	}
	c.SendDreadReply(addr, 1, name, stat)
	return 0
}

// handleGetstat parses GETSTAT_REQ, calls fs.Getstat, sends GETSTAT_REPLY.
func (c *Connection) handleGetstat(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 4 {
		c.SendGetstatReply(addr, -EINVAL, StatInfo{})
		return -EINVAL
	}
	pathEnd := bytes.IndexByte(payload[4:], 0)
	if pathEnd < 0 {
		pathEnd = len(payload) - 4
	}
	path := string(payload[4 : 4+pathEnd])

	stat, err := c.fs.Getstat(path)
	if err != nil {
		errCode := -errToErrno(err)
		c.SendGetstatReply(addr, errCode, StatInfo{})
		return int(errCode)
	}
	c.SendGetstatReply(addr, 0, stat)
	return 0
}

// handleMkdir parses MKDIR_REQ, calls fs.Mkdir, sends RESULT_REPLY.
func (c *Connection) handleMkdir(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 4 {
		c.SendResultReplyOnly(addr, -EINVAL)
		return -EINVAL
	}
	pathEnd := bytes.IndexByte(payload[4:], 0)
	if pathEnd < 0 {
		pathEnd = len(payload) - 4
	}
	path := string(payload[4 : 4+pathEnd])

	if err := c.fs.Mkdir(path); err != nil {
		errCode := -errToErrno(err)
		c.SendResultReplyOnly(addr, errCode)
		return int(errCode)
	}
	c.SendResultReplyOnly(addr, 0)
	return 0
}

// handleRemove parses REMOVE_REQ, calls fs.Remove, sends RESULT_REPLY.
func (c *Connection) handleRemove(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 4 {
		c.SendResultReplyOnly(addr, -EINVAL)
		return -EINVAL
	}
	pathEnd := bytes.IndexByte(payload[4:], 0)
	if pathEnd < 0 {
		pathEnd = len(payload) - 4
	}
	path := string(payload[4 : 4+pathEnd])

	if err := c.fs.Remove(path); err != nil {
		errCode := -errToErrno(err)
		c.SendResultReplyOnly(addr, errCode)
		return int(errCode)
	}
	c.SendResultReplyOnly(addr, 0)
	return 0
}

// handleRmdir parses RMDIR_REQ, calls fs.Rmdir, sends RESULT_REPLY.
func (c *Connection) handleRmdir(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 4 {
		c.SendResultReplyOnly(addr, -EINVAL)
		return -EINVAL
	}
	pathEnd := bytes.IndexByte(payload[4:], 0)
	if pathEnd < 0 {
		pathEnd = len(payload) - 4
	}
	path := string(payload[4 : 4+pathEnd])

	if err := c.fs.Rmdir(path); err != nil {
		errCode := -errToErrno(err)
		c.SendResultReplyOnly(addr, errCode)
		return int(errCode)
	}
	c.SendResultReplyOnly(addr, 0)
	return 0
}

// handleBread parses BREAD_REQ, calls fs.Bread, sends RESULT_REPLY + data.
func (c *Connection) handleBread(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 16 {
		c.SendReadResult(addr, -EINVAL, nil)
		return -EINVAL
	}
	sectorCount := binary.LittleEndian.Uint16(payload[2:4])
	handle := int32(binary.LittleEndian.Uint32(payload[4:8]))
	sectorNrLo := binary.LittleEndian.Uint32(payload[8:12])
	sectorNrHi := binary.LittleEndian.Uint32(payload[12:16])
	sectorNr := int64(sectorNrHi)<<32 | int64(sectorNrLo)

	c.Lock()
	defer c.Unlock()
	data, err := c.fs.Bread(handle, sectorNr, sectorCount, c.dataBuffer[:cap(c.dataBuffer)])
	if err != nil {
		errCode := -errToErrno(err)
		c.SendReadResult(addr, errCode, nil)
		return int(errCode)
	}
	c.SendReadResult(addr, int32(len(data)), data)
	return len(data)
}

// handleBwriteReq parses BWRITE_REQ, calls fs.BwriteStart; if first chunk in payload, parses and calls fs.WriteChunk; if done, calls fs.CompleteWrite and sends WRITE_DONE.
func (c *Connection) handleBwriteReq(addr *net.UDPAddr, payload []byte) int {
	if len(payload) < 16 {
		c.SendACK(addr, true)
		return 0
	}
	sectorCount := binary.LittleEndian.Uint16(payload[2:4])
	handle := int32(binary.LittleEndian.Uint32(payload[4:8]))
	sectorNrLo := binary.LittleEndian.Uint32(payload[8:12])
	sectorNrHi := binary.LittleEndian.Uint32(payload[12:16])
	sectorNr := int64(sectorNrHi)<<32 | int64(sectorNrLo)

	if err := c.fs.BwriteStart(handle, sectorNr, sectorCount); err != nil {
		errCode := -errToErrno(err)
		c.SendWriteDone(addr, errCode)
		return int(errCode)
	}
	c.setWriteHandle(handle)

	if len(payload) <= 16 {
		c.SendACK(addr, true)
		return 0
	}
	chunkPayload := payload[16:]
	if len(chunkPayload) < 8 {
		c.SendACK(addr, true)
		return 0
	}
	chunkNr := binary.LittleEndian.Uint16(chunkPayload[2:4])
	chunkSize := binary.LittleEndian.Uint16(chunkPayload[4:6])
	totalChunks := binary.LittleEndian.Uint16(chunkPayload[6:8])
	chunkData := chunkPayload[8:]
	if int(chunkSize) < len(chunkData) {
		chunkData = chunkData[:chunkSize]
	}
	done, err := c.fs.WriteChunk(handle, chunkNr, chunkSize, totalChunks, chunkData)
	if err != nil {
		errCode := -errToErrno(err)
		c.SendWriteDone(addr, errCode)
		return int(errCode)
	}
	if done {
		n, err := c.fs.CompleteWrite(handle)
		if err != nil {
			errCode := -errToErrno(err)
			c.SendWriteDone(addr, errCode)
			return int(errCode)
		}
		c.SendWriteDone(addr, n)
		return len(payload)
	}
	c.SendACK(addr, true)
	return len(payload)
}
