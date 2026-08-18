package compression

import (
	"fmt"
	"io"
	"os"
)

// CachedReader wraps a BlockReader with an LRU-style block cache and implements FileObject interface
type CachedReader struct {
	br        BlockReader
	info      os.FileInfo
	cache     *blockCache
	blockSize int
	numBlocks int
	size      int64
	pos       int64
}

// NewCachedReader wraps a BlockReader with the default cache size (32 blocks).
func NewCachedReader(br BlockReader, cacheSize int, info os.FileInfo) *CachedReader {
	if cacheSize <= 0 {
		cacheSize = 32
	}
	return &CachedReader{
		br:        br,
		blockSize: br.BlockSize(),
		numBlocks: br.NumBlocks(),
		size:      br.UncompressedSize(),
		cache:     newBlockCache(cacheSize),
		info:      info,
	}
}

func (c *CachedReader) Read(p []byte) (n int, err error) {
	if c.pos >= c.size {
		return 0, io.EOF
	}
	remaining := c.size - c.pos
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	read := 0
	for len(p) > 0 && c.pos < c.size {
		blockIdx := int(c.pos / int64(c.blockSize))
		offsetInBlock := int(c.pos % int64(c.blockSize))
		lastBlockSize := c.blockSize
		if blockIdx == c.numBlocks-1 && c.size%int64(c.blockSize) != 0 {
			lastBlockSize = int(c.size % int64(c.blockSize))
		}
		avail := lastBlockSize - offsetInBlock
		if avail <= 0 {
			break
		}
		toRead := len(p)
		if toRead > avail {
			toRead = avail
		}
		block, err := c.getBlock(blockIdx)
		if err != nil {
			return read, err
		}
		copy(p, block[offsetInBlock:offsetInBlock+toRead])
		p = p[toRead:]
		read += toRead
		c.pos += int64(toRead)
	}
	return read, nil
}

func (c *CachedReader) Write([]byte) (int, error) {
	return 0, nil
}

func (c *CachedReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		c.pos = offset
	case io.SeekCurrent:
		c.pos += offset
	case io.SeekEnd:
		c.pos = c.size + offset
	default:
		return c.pos, fmt.Errorf("invalid whence %d", whence)
	}
	if c.pos < 0 {
		c.pos = 0
	}
	if c.pos > c.size {
		c.pos = c.size
	}
	return c.pos, nil
}

func (c *CachedReader) Close() error {
	return c.br.Close()
}

func (c *CachedReader) Stat() (os.FileInfo, error) {
	return c.info, nil
}

func (c *CachedReader) Name() string {
	return c.info.Name()
}

func (c *CachedReader) getBlock(blockIdx int) ([]byte, error) {
	if b, ok := c.cache.get(blockIdx); ok {
		return b, nil
	}
	b, err := c.br.ReadBlock(blockIdx)
	if err != nil {
		return nil, err
	}
	c.cache.put(blockIdx, b)
	return b, nil
}
