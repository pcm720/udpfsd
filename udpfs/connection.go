// UPDFS implementation
// See docs/UDPFS.md for details
package udpfs

import (
	"net"
	"os"
	"strings"
	"sync"

	"github.com/pcm720/udpfsd/udprdma"
)

// Connection wraps a UDPRDMA session and an FS, and provides UDPFS reply encoding, sending, and request dispatch.
// Create with NewConnection; then call HandlePayload for each received UDPFS data payload.
type Connection struct {
	fs   FS
	sess *udprdma.Session
	*metricCollector

	usedHandles       map[int32]cachedHandle
	activeWriteHandle int32

	sync.Mutex
	dataBuffer [128 * 1024]byte // 128 KB read buffer
	verbose    bool
}

type cachedHandle struct {
	path  string
	flag  int
	reset bool
	isDir bool
}

// NewConnection returns a Connection that sends via the given session and dispatches requests to fs.
func NewConnection(sess *udprdma.Session, fs FS, verbose bool, collectMetrics bool) *Connection {
	c := &Connection{
		sess:              sess,
		fs:                fs,
		verbose:           verbose,
		usedHandles:       make(map[int32]cachedHandle, 32),
		activeWriteHandle: -1,
	}
	sess.SetResetCallback(c.ResetPeer)
	if collectMetrics {
		c.metricCollector = newMetricCollector()
	}
	return c
}

func (c *Connection) GetUDPRDMASession() *udprdma.Session {
	return c.sess
}

func (c *Connection) GetMetrics() ConnectionStats {
	return c.metricCollector.GetMetrics()
}

// SendACK sends an ACK or NACK packet (no payload).
func (c *Connection) SendACK(addr *net.UDPAddr, ack bool) {
	c.sess.SendACK(ack)
}

// SendData sends a single DATA packet with full payload and FIN.
func (c *Connection) SendData(addr *net.UDPAddr, payload []byte) {
	c.sess.SendData(payload)
}

// SendOpenReply sends an OPEN_REPLY.
func (c *Connection) SendOpenReply(addr *net.UDPAddr, handle int32, st StatInfo) {
	c.sess.SendData(PackOpenReply(handle, st))
}

// SendReadResult sends a RESULT_REPLY header plus optional data (for READ).
func (c *Connection) SendReadResult(addr *net.UDPAddr, result int32, data []byte) {
	if data == nil {
		data = []byte{}
	}
	c.sess.SendRawDataWithHeader(PackResultReply(result), data)
}

// SendWriteDone sends a WRITE_DONE.
func (c *Connection) SendWriteDone(addr *net.UDPAddr, result int32) {
	c.clearWriteHandle()
	c.sess.SendData(PackWriteDone(result))
}

// SendLseekReply sends an LSEEK_REPLY.
func (c *Connection) SendLseekReply(addr *net.UDPAddr, position int64) {
	c.sess.SendData(PackLseekReply(position))
}

// SendDreadReply sends a DREAD_REPLY.
func (c *Connection) SendDreadReply(addr *net.UDPAddr, result int32, name string, st StatInfo) {
	c.sess.SendData(PackDreadReply(result, name, st))
}

// SendGetstatReply sends a GETSTAT_REPLY.
func (c *Connection) SendGetstatReply(addr *net.UDPAddr, result int32, st StatInfo) {
	c.sess.SendData(PackGetstatReply(result, st))
}

// SendCloseReply sends a CLOSE_REPLY.
func (c *Connection) SendCloseReply(addr *net.UDPAddr, result int32) {
	c.sess.SendData(PackCloseReply(result))
}

// SendResultReplyOnly sends an 8-byte RESULT_REPLY (mkdir/remove/rmdir).
func (c *Connection) SendResultReplyOnly(addr *net.UDPAddr, result int32) {
	c.sess.SendData(PackResultReply(result))
}

// Returns cached file handle if peer was reset and tries to open the same file again
func (c *Connection) lookupHandle(path string, flag int) (int32, bool) {
	c.Lock()
	defer c.Unlock()
	for fsHandle, h := range c.usedHandles {
		if h.reset && h.path == path && h.flag == flag {
			h.reset = false
			c.usedHandles[fsHandle] = h
			return fsHandle, true
		}
	}
	return 0, false
}

// Adds handle to peer handle cache
func (c *Connection) addHandle(handle int32, path string, flag int, isDir bool) {
	c.Lock()
	defer c.Unlock()
	c.usedHandles[handle] = cachedHandle{
		path:  strings.Clone(path),
		flag:  flag,
		reset: false,
		isDir: isDir,
	}
}

// Removes handle from cache
func (c *Connection) removeHandle(handle int32) {
	c.Lock()
	defer c.Unlock()
	delete(c.usedHandles, handle)
	if c.activeWriteHandle == handle {
		c.activeWriteHandle = -1
	}
}

// Resets all peer handles
func (c *Connection) ResetPeer() {
	c.Lock()
	defer c.Unlock()

	for fsHandle, h := range c.usedHandles {
		c.fs.Lseek(fsHandle, 0, os.SEEK_SET)
		h.reset = true
		c.usedHandles[fsHandle] = h
	}
}

// Closes all peer handles
func (c *Connection) Close() {
	c.Lock()
	defer c.Unlock()

	for fsHandle := range c.usedHandles {
		c.fs.Close(fsHandle)
		delete(c.usedHandles, fsHandle)
	}
	c.sess.Close()
}

func (c *Connection) getWriteHandle() int32 {
	c.Lock()
	defer c.Unlock()
	return c.activeWriteHandle
}

func (c *Connection) setWriteHandle(handle int32) {
	c.Lock()
	defer c.Unlock()
	c.activeWriteHandle = handle
}

func (c *Connection) clearWriteHandle() {
	c.Lock()
	defer c.Unlock()
	c.activeWriteHandle = -1
}
