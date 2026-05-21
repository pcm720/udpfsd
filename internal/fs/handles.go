package fs

import (
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/pcm720/udpfsd/internal/fs/interfaces"
	"github.com/pcm720/udpfsd/udpfs"
)

// handle is either a file or a directory.
type handle interface {
	io.Closer
}

type blockDeviceHandle struct {
	*fileHandle
	totalSectorCount int64
}

type fileHandle struct {
	obj     interfaces.FileObject
	Name    func() string
	Read    func(p []byte) (n int, err error)
	Write   func(p []byte) (n int, err error)
	Seek    func(offset int64, whence int) (int64, error)
	Stat    func() (os.FileInfo, error)
	closeFn func() error

	wr writeState
	sync.Mutex

	readOnly bool
}
type writeState struct {
	dataBuffer     []byte
	sectorNumber   int64
	sectorCount    uint16
	totalChunks    uint16
	receivedChunks uint16
	blockWrite     bool
}

func (f *fileHandle) Close() error {
	if f.closeFn != nil {
		return f.closeFn()
	}
	return nil
}

type dirHandle struct {
	dirPath string
	entries []os.DirEntry
	index   int
	sync.Mutex
}

func (d *dirHandle) Close() error { return nil }

func (s *Backend) allocHandle(h handle) int32 {
	s.Lock()
	defer s.Unlock()
	oldestHandle, oldestHandleTimestamp := 0, time.Now()
	for i, handle := range s.handles {
		// Try to find the first free handle
		if handle == nil {
			s.handles[i] = h
			s.lastUsed[i] = time.Now()

			// Handle 0 is reserved for block device, so our external handles start with 1
			return int32(i + 1)
		}
		// Determine the oldest handle
		if s.lastUsed[i].Before(oldestHandleTimestamp) {
			oldestHandle = i
			oldestHandleTimestamp = s.lastUsed[i]
		}
	}

	if time.Since(oldestHandleTimestamp) >= handleMaxLastUsed {
		// If the oldest handle hasn't been used for at least handleMaxLastUsed,
		// close and reallocate its index to the new handle
		log.Printf("fs: no free handles left, evicting handle %d", oldestHandle+1)
		s.handles[oldestHandle].Close()
		s.handles[oldestHandle] = h
		s.lastUsed[oldestHandle] = time.Now()
		return int32(oldestHandle + 1)
	}
	log.Println("fs: no free handles left")
	return -udpfs.EMFILE
}

func (s *Backend) freeHandle(handle int32) bool {
	if handle == udpfs.BlockDeviceHandle {
		// Block device is opened for server lifetime
		return true
	}
	s.Lock()
	defer s.Unlock()

	// Handle 0 is reserved for block device, so our external handles start with 1
	handle = handle - 1

	if h := s.handles[handle]; h != nil {
		h.Close()
		s.handles[handle] = nil
		return true
	}
	return true
}

func (s *Backend) getFile(handle int32) *fileHandle {
	if handle < 0 {
		return nil
	}

	if handle == udpfs.BlockDeviceHandle {
		if s.bdHandle == nil {
			return nil
		}
		return s.bdHandle.fileHandle
	}
	s.Lock()
	defer s.Unlock()

	// Handle 0 is reserved for block device, so our external handles start with 1
	handle = handle - 1

	h := s.handles[handle]
	if h == nil {
		return nil
	}
	fh, ok := h.(*fileHandle)
	if !ok {
		return nil
	}
	s.lastUsed[handle] = time.Now()
	return fh
}

func (s *Backend) getFileByPath(hostPath string) *fileHandle {
	s.Lock()
	defer s.Unlock()

	for idx, f := range s.handles {
		if fh, ok := f.(*fileHandle); ok {
			if fh.Name() == hostPath {
				s.lastUsed[idx] = time.Now()
				return fh
			}
		}
	}
	return nil
}

// getFileState returns whether the file at hostPath is currently open, and if so whether for writing.
func (s *Backend) getFileState(hostPath string) (open bool, readOnly bool) {
	s.Lock()
	defer s.Unlock()

	for _, f := range s.handles {
		if fh, ok := f.(*fileHandle); ok && fh.Name() == hostPath {
			return true, fh.readOnly
		}
	}
	return false, true
}

func (s *Backend) getDir(handle int32) *dirHandle {
	s.Lock()
	defer s.Unlock()

	// Handle 0 is reserved for block device, so our external handles start with 1
	handle = handle - 1

	h := s.handles[handle]
	if h == nil {
		return nil
	}
	dh, ok := h.(*dirHandle)
	if !ok {
		return nil
	}
	s.lastUsed[handle] = time.Now()
	return dh
}

func (s *Backend) newFileHandle(f interfaces.FileObject, readOnly bool) handle {
	return &fileHandle{
		obj:      f,
		Read:     f.Read,
		Write:    f.Write,
		Seek:     f.Seek,
		Name:     f.Name,
		Stat:     f.Stat,
		closeFn:  f.Close,
		readOnly: readOnly,
	}
}
