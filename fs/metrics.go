package fs

import (
	"github.com/pcm720/udpfsd/fs/compression"
)

// Metrics is a point-in-time snapshot of the backend's mount configuration.
type Metrics struct {
	FSRoot             string
	BlockDevice        string
	SectorSize         int
	TotalSectors       int64 // 0 if no block device is mounted
	ReadOnly           bool
	CompressionFormats []string // nil if compression is disabled
}

// Stats returns a snapshot of the backend's mount configuration.
func (s *Backend) Stats() Metrics {
	s.Lock()
	defer s.Unlock()
	metrics := Metrics{
		FSRoot:      s.fsRoot,
		BlockDevice: s.blockDevice,
		SectorSize:  s.sectorSize,
		ReadOnly:    s.readOnly,
	}
	if s.bdHandle != nil {
		metrics.TotalSectors = s.bdHandle.totalSectorCount
	}
	if s.enableCompression {
		metrics.CompressionFormats = compression.GetSupportedFormats()
	}
	return metrics
}
