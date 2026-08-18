package zso

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pcm720/udpfsd/fs/compression/common"
	"github.com/pierrec/lz4/v4"
)

// ZISO/ZSO magic
var (
	zsoMagic   = []byte{'Z', 'S', 'O', 0}
	zisoMagic  = []byte{'Z', 'I', 'S', 'O'}
	Extensions = []string{".zso", ".ziso"}
)

// ZSO implements BlockReader for ZSO/ZISO (LZ4 block-based) format.
type ZSO struct {
	f         *os.File
	stat      *common.CompressedInfo
	offsets   []uint32
	blockSize int
	numBlocks int
	size      int64
	align     uint8
	ziso      bool
}

// Open opens a ZSO or ZISO file and parses its header.
func Open(path string) (*ZSO, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	z := &ZSO{f: f}
	if err := z.parseHeader(); err != nil {
		f.Close()
		return nil, err
	}
	return z, nil
}

func (z *ZSO) parseHeader() error {
	_, err := z.f.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	b := make([]byte, 24)
	if _, err := io.ReadFull(z.f, b); err != nil {
		return err
	}
	magic := b[0:4]

	if bytes.Equal(magic, zisoMagic) {
		z.ziso = true
	} else if !bytes.Equal(magic, zsoMagic) {
		return fmt.Errorf("invalid ZSO magic: %q", magic)
	}

	headerSize := binary.LittleEndian.Uint32(b[4:8])
	z.size = int64(binary.LittleEndian.Uint64(b[8:16]))
	z.blockSize = int(binary.LittleEndian.Uint32(b[16:20]))
	if z.ziso {
		z.align = b[21]
	}
	if z.blockSize == 0 {
		return fmt.Errorf("invalid ZSO: block_size is 0")
	}
	z.numBlocks = int((z.size + int64(z.blockSize) - 1) / int64(z.blockSize))
	numOffsetEntries := z.numBlocks + 1
	if !z.ziso {
		numOffsetEntries = z.numBlocks
	}
	_, err = z.f.Seek(int64(headerSize), io.SeekStart)
	if err != nil {
		return err
	}
	offsetBytes := make([]byte, numOffsetEntries*4)
	if _, err := io.ReadFull(z.f, offsetBytes); err != nil {
		return err
	}
	z.offsets = make([]uint32, numOffsetEntries)
	for i := 0; i < numOffsetEntries; i++ {
		z.offsets[i] = binary.LittleEndian.Uint32(offsetBytes[i*4 : (i+1)*4])
	}

	var modTime time.Time
	if fstat, err := z.f.Stat(); err == nil {
		modTime = fstat.ModTime()
	}
	z.stat = common.NewCompressedInfo(filepath.Base(z.f.Name()), int64(z.size), modTime)
	return nil
}

func (z *ZSO) BlockSize() int          { return z.blockSize }
func (z *ZSO) NumBlocks() int          { return z.numBlocks }
func (z *ZSO) UncompressedSize() int64 { return z.size }

func (z *ZSO) Stat() os.FileInfo {
	return z.stat
}

func (z *ZSO) ReadBlock(blockIdx int) ([]byte, error) {
	if blockIdx < 0 || blockIdx >= z.numBlocks {
		return nil, fmt.Errorf("block index %d out of range", blockIdx)
	}
	rawOffset := z.offsets[blockIdx]
	var offset int64
	var compressedSize int
	if z.ziso {
		nextRaw := z.offsets[blockIdx+1]
		offset = int64(rawOffset&0x7FFFFFFF) << z.align
		nextOffset := int64(nextRaw&0x7FFFFFFF) << z.align
		compressedSize = int(nextOffset - offset)
	} else {
		if blockIdx+1 < z.numBlocks {
			compressedSize = int(z.offsets[blockIdx+1] - rawOffset)
		} else {
			info, err := z.f.Stat()
			if err != nil {
				return nil, err
			}
			compressedSize = int(info.Size() - int64(rawOffset))
		}
		offset = int64(rawOffset)
	}
	uncompressed := (rawOffset & 0x80000000) != 0

	_, err := z.f.Seek(offset, io.SeekStart)
	if err != nil {
		return nil, err
	}
	compressed := make([]byte, compressedSize)
	if _, err := io.ReadFull(z.f, compressed); err != nil {
		return nil, err
	}
	if uncompressed {
		out := make([]byte, z.blockSize)
		copy(out, compressed)
		return out, nil
	}
	// LZ4 with OPL-style retry (trim trailing byte for alignment padding)
	for len(compressed) > 0 {
		out := make([]byte, z.blockSize)
		n, err := lz4.UncompressBlock(compressed, out)
		if err == nil {
			return out[:n], nil
		}
		compressed = compressed[:len(compressed)-1]
	}
	return nil, fmt.Errorf("LZ4 decompression failed for block %d", blockIdx)
}

func (z *ZSO) Close() error {
	if z.f == nil {
		return nil
	}
	err := z.f.Close()
	z.f = nil
	return err
}

func GetStat(path string) *common.CompressedInfo {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}

	b := make([]byte, 16)
	if _, err := io.ReadFull(f, b); err != nil {
		return nil
	}
	magic := b[0:4]
	if !bytes.Equal(magic, zisoMagic) && !bytes.Equal(magic, zsoMagic) {
		return nil
	}

	size := int64(binary.LittleEndian.Uint64(b[8:16]))

	var modTime time.Time
	if fstat, err := f.Stat(); err == nil {
		modTime = fstat.ModTime()
	}
	return common.NewCompressedInfo(filepath.Base(path), int64(size), modTime)
}
