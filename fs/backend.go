// Filesystem backend for UDPFS
package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pcm720/udpfsd/udpfs"
)

const (
	maxHandles        int32 = 64
	handleMaxLastUsed       = time.Hour
)

// Backend implements udpfs.FS for a root directory and optional block device.
type Backend struct {
	// Handles
	lastUsed [maxHandles]time.Time // Last used timestamp for every handle
	handles  [maxHandles]handle    // Handle
	bdHandle *blockDeviceHandle

	// Device data
	fsRoot               string
	blockDevice          string
	sectorSize           int
	compressionCacheSize int

	sync.Mutex
	readOnly          bool
	enableCompression bool
}

type BackendOptFunc func(s *Backend)

func WithFSRoot(path string) func(s *Backend) {
	return func(s *Backend) {
		if path != "" {
			s.fsRoot = path
		}
	}
}

func WithBlockDevice(path string) func(s *Backend) {
	return func(s *Backend) {
		if path != "" {
			s.blockDevice = path
		}
	}
}

func WithSectorSize(size int) func(s *Backend) {
	return func(s *Backend) {
		if size != 0 {
			s.sectorSize = size
		}
	}
}

func WithReadOnly() func(s *Backend) {
	return func(s *Backend) {
		s.readOnly = true
	}
}

func WithCompression() func(s *Backend) {
	return func(s *Backend) {
		s.enableCompression = true
	}
}

func WithCompressionCacheSize(ccsize int) func(s *Backend) {
	return func(s *Backend) {
		if ccsize != 0 {
			s.compressionCacheSize = ccsize
		}
	}
}

// Ensure Backend implements udpfs.FS at compile time.
var _ udpfs.FS = (*Backend)(nil)

// FS implementation assumes block device handle is 0
const assertedBlockDeviceHandle = 0
const (
	_ uint = assertedBlockDeviceHandle - udpfs.BlockDeviceHandle
	_ uint = udpfs.BlockDeviceHandle - assertedBlockDeviceHandle
)

// NewBackend initializes the filesystem implementation
func NewBackend(opts ...BackendOptFunc) (*Backend, error) {
	s := &Backend{
		sectorSize:           512,
		readOnly:             false,
		enableCompression:    false,
		compressionCacheSize: 0,
	}
	for _, o := range opts {
		o(s)
	}

	if s.fsRoot != "" {
		abs, err := filepath.Abs(s.fsRoot)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", s.fsRoot)
		}
		s.fsRoot = abs
	}
	if s.blockDevice != "" {
		mode := os.O_RDONLY
		if !s.readOnly {
			mode = os.O_RDWR
		}
		f, err := os.OpenFile(s.blockDevice, mode, 0)
		if err != nil {
			return nil, fmt.Errorf("block device: %w", err)
		}

		info, _ := f.Stat()
		s.bdHandle = &blockDeviceHandle{
			fileHandle:       s.newFileHandle(f, s.readOnly).(*fileHandle),
			totalSectorCount: info.Size() / int64(s.sectorSize),
		}
	}
	// Print information about the mounted filesystem/block device to stdout
	s.PrintFSInfo()
	return s, nil
}

func (s *Backend) Shutdown() {
	for fd := range s.handles {
		s.freeHandle(int32(fd))
	}
}
