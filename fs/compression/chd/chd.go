// CHD (Compressed Hunks of Data) support via libchdr (CGo).
//
// Requires libchdr:
//   Debian/Ubuntu: apt install libchdr-dev
//   Arch: install libchdr-git from AUR
//   Build from source: https://github.com/rtissera/libchdr
//
//go:build cgo && !nochd

package chd

/*
#cgo LDFLAGS: -lchdr
#include "chd_libchdr.h"
#include <stdlib.h>
*/
import "C"
import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/pcm720/udpfsd/fs/compression/common"
)

// Available is true when built with CGo and libchdr.
const Available = true

var Extensions = []string{".chd"}

const (
	cdFrameSize = 2448 // 2352 sector + 96 subcode
	cdUserData  = 2048
)

var cdCodecs = map[uint32]bool{
	0x63646c7a: true, // cdlz
	0x63647a6c: true, // cdzl
	0x6364666c: true, // cdfl
}

var cdSync = []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00}

func extractUserData(sector []byte) []byte {
	out := make([]byte, cdUserData)
	if len(sector) < cdUserData {
		copy(out, sector)
		return out
	}
	var offset int
	if len(sector) >= 16 && bytesEqual(sector[0:12], cdSync) {
		if sector[15] == 2 {
			offset = 24 // Mode 2 Form 1
		} else {
			offset = 16 // Mode 1
		}
	}
	end := offset + cdUserData
	if end > len(sector) {
		copy(out, sector[offset:])
		return out
	}
	copy(out, sector[offset:end])
	return out
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CHD implements BlockReader for CHD v5 using libchdr.
type CHD struct {
	path          string
	chd           *C.struct_chd_file
	hunkBuf       []byte
	hunkSize      int
	blockSize     int
	numBlocks     int
	size          int64
	isCDFormat    bool
	framesPerHunk int
	stat          *common.CompressedInfo
}

// Open opens a CHD v5 file via libchdr.
func Open(path string) (*CHD, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	var chd *C.struct_chd_file
	ret := C.chd_open(cpath, C.CHD_OPEN_READ, nil, &chd)
	if ret != 0 {
		return nil, fmt.Errorf("chd_open failed: %d", ret)
	}

	c := &CHD{path: path, chd: chd}
	if err := c.parseHeader(); err != nil {
		C.chd_close(chd)
		return nil, err
	}
	c.hunkBuf = make([]byte, c.hunkSize)
	return c, nil
}

func (c *CHD) parseHeader() error {
	f, err := os.Open(c.path)
	if err != nil {
		return err
	}
	defer f.Close()

	header := make([]byte, 124)
	if _, err := io.ReadFull(f, header); err != nil {
		return err
	}
	if len(header) < 64 || string(header[0:8]) != "MComprHD" {
		return fmt.Errorf("not a valid CHD file")
	}
	version := binary.BigEndian.Uint32(header[12:16])
	if version != 5 {
		return fmt.Errorf("unsupported CHD version %d (only v5)", version)
	}

	compressors := make([]uint32, 4)
	for i := 0; i < 4; i++ {
		compressors[i] = binary.BigEndian.Uint32(header[16+i*4 : 20+i*4])
	}
	logicalBytes := binary.BigEndian.Uint64(header[32:40])
	hunkSize := binary.BigEndian.Uint32(header[56:60])
	unitSize := binary.BigEndian.Uint32(header[60:64])

	if hunkSize == 0 {
		return fmt.Errorf("CHD hunkbytes is 0")
	}

	c.hunkSize = int(hunkSize)
	c.blockSize = int(hunkSize)
	c.numBlocks = int((logicalBytes + uint64(hunkSize) - 1) / uint64(hunkSize))

	isCD := unitSize == cdFrameSize
	for _, comp := range compressors {
		if comp != 0 && cdCodecs[comp] {
			isCD = true
			break
		}
	}
	c.isCDFormat = isCD && hunkSize%cdFrameSize == 0
	if c.isCDFormat {
		c.framesPerHunk = int(hunkSize / cdFrameSize)
		totalFrames := c.numBlocks * c.framesPerHunk
		c.size = int64(totalFrames * cdUserData)
		c.blockSize = c.framesPerHunk * cdUserData
	} else {
		c.size = int64(logicalBytes)
	}

	var modTime time.Time
	if fstat, err := f.Stat(); err == nil {
		modTime = fstat.ModTime()
	}
	c.stat = common.NewCompressedInfo(filepath.Base(f.Name()), int64(c.size), modTime)
	return nil
}

func (c *CHD) BlockSize() int          { return c.blockSize }
func (c *CHD) NumBlocks() int          { return c.numBlocks }
func (c *CHD) UncompressedSize() int64 { return c.size }

func (c *CHD) Stat() os.FileInfo {
	return c.stat
}

func (c *CHD) ReadBlock(blockIdx int) ([]byte, error) {
	if blockIdx < 0 || blockIdx >= c.numBlocks {
		return nil, fmt.Errorf("block index %d out of range", blockIdx)
	}
	ret := C.chd_read(c.chd, C.uint(blockIdx), unsafe.Pointer(&c.hunkBuf[0]))
	if ret != 0 {
		return nil, fmt.Errorf("chd_read failed for hunk %d: %d", blockIdx, ret)
	}
	if !c.isCDFormat {
		out := make([]byte, c.hunkSize)
		copy(out, c.hunkBuf)
		return out, nil
	}
	// CD format: extract 2048-byte user data from each 2352-byte sector in the hunk
	out := make([]byte, 0, c.blockSize)
	for i := 0; i < c.framesPerHunk; i++ {
		start := i * cdFrameSize
		sector := c.hunkBuf[start : start+2352]
		out = append(out, extractUserData(sector)...)
	}
	return out, nil
}

func (c *CHD) Close() error {
	if c.chd == nil {
		return nil
	}
	C.chd_close(c.chd)
	c.chd = nil
	return nil
}

func GetStat(path string) *common.CompressedInfo {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	header := make([]byte, 124)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil
	}
	if len(header) < 64 || string(header[0:8]) != "MComprHD" {
		return nil
	}
	version := binary.BigEndian.Uint32(header[12:16])
	if version != 5 {
		return nil
	}

	compressors := make([]uint32, 4)
	for i := 0; i < 4; i++ {
		compressors[i] = binary.BigEndian.Uint32(header[16+i*4 : 20+i*4])
	}
	logicalBytes := binary.BigEndian.Uint64(header[32:40])
	hunkSize := binary.BigEndian.Uint32(header[56:60])
	unitSize := binary.BigEndian.Uint32(header[60:64])

	if hunkSize == 0 {
		return nil
	}

	numBlocks := int((logicalBytes + uint64(hunkSize) - 1) / uint64(hunkSize))

	isCD := (unitSize == cdFrameSize)
	for _, comp := range compressors {
		if comp != 0 && cdCodecs[comp] {
			isCD = true
			break
		}
	}

	var fileSize int64
	if isCD && hunkSize%cdFrameSize == 0 {
		fileSize = int64((numBlocks * int(hunkSize/cdFrameSize)) * cdUserData)
	} else {
		fileSize = int64(logicalBytes)
	}

	var modTime time.Time
	if fstat, err := f.Stat(); err == nil {
		modTime = fstat.ModTime()
	}
	return common.NewCompressedInfo(filepath.Base(path), fileSize, modTime)
}
