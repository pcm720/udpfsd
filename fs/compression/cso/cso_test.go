package cso

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const testDir = "../test/"

func TestOpenValidCSO(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	if c.NumBlocks() <= 0 {
		t.Errorf("Expected numBlocks > 0, got %d", c.NumBlocks())
	}
	if c.BlockSize() <= 0 {
		t.Errorf("Expected blockSize > 0, got %d", c.BlockSize())
	}
	if c.UncompressedSize() <= 0 {
		t.Errorf("Expected uncompressedSize > 0, got %d", c.UncompressedSize())
	}

	info := c.Stat()
	if info == nil {
		t.Fatal("Stat() returned nil")
	}
	if info.Name() != "test.cso" {
		t.Errorf("Expected name 'test.cso', got '%s'", info.Name())
	}
}

func TestReadBlocksCSO(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	numBlocks := c.NumBlocks()
	blockSize := c.BlockSize()
	var totalRead int64 = 0

	decompressedData := []byte{}
	for i := 0; i < numBlocks; i++ {
		data, err := c.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d: %v", i, err)
			continue
		}
		totalRead += int64(len(data))
		if len(data) > blockSize {
			t.Errorf("Block %d size %d exceeds expected blockSize %d", i, len(data), blockSize)
		}
		decompressedData = append(decompressedData, data...)
	}

	if totalRead != c.UncompressedSize() {
		t.Errorf("Total bytes read (%d) doesn't match uncompressed size (%d)", totalRead, c.UncompressedSize())
	}

	ref, err := os.ReadFile(filepath.Join(testDir, "test.iso"))
	if !bytes.Equal(ref, decompressedData) {
		t.Errorf("Decompressed file does not match the reference ISO")
	}
}

func TestReadBlockInvalidIndex(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	_, err = c.ReadBlock(-1)
	if err == nil {
		t.Error("Expected error for negative block index")
	}

	_, err = c.ReadBlock(c.NumBlocks())
	if err == nil {
		t.Errorf("Expected error for block index >= numBlocks (%d)", c.NumBlocks())
	}
}

func TestGetStatValidCSO(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	info := GetStat(path)
	if info == nil {
		t.Fatal("GetStat returned nil for valid CSO file")
	}
	if info.Name() != "test.cso" {
		t.Errorf("Expected name 'test.cso', got '%s'", info.Name())
	}
	if info.Size() <= 0 {
		t.Errorf("Expected size > 0, got %d", info.Size())
	}
	if info.IsDir() {
		t.Error("GetStat should return non-directory file")
	}
}

func TestGetStatInvalidFile(t *testing.T) {
	info := GetStat(filepath.Join(testDir, "nonexistent.cso"))
	if info != nil {
		t.Error("Expected nil for nonexistent file")
	}

	info = GetStat(filepath.Join(testDir, "test_cd.chd"))
	if info != nil {
		t.Error("Expected nil for non-CSO file")
	}

	info = GetStat(filepath.Join(testDir, "test.iso"))
	if info != nil {
		t.Error("Expected nil for ISO file (not CSO)")
	}
}

func TestOpenNonExistentFile(t *testing.T) {
	_, err := Open("/nonexistent/path/to/file.cso")
	if err == nil {
		t.Error("Expected error when opening nonexistent file")
	}
}

func TestCloseMultipleTimes(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}

	err = c.Close()
	if err != nil {
		t.Errorf("First Close failed: %v", err)
	}

	err = c.Close()
	if err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
}

func TestBlockSizeConsistency(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	blockSize := c.BlockSize()
	for i := 0; i < c.NumBlocks(); i++ {
		data, err := c.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d: %v", i, err)
			continue
		}
		if len(data) > blockSize {
			t.Errorf("Block %d size %d exceeds expected blockSize %d", i, len(data), blockSize)
		}
	}
}

func TestHeaderParsing(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	if c.NumBlocks() == 0 {
		t.Error("Expected numBlocks > 0 after header parsing")
	}
	if len(c.offsets) == 0 {
		t.Error("Expected offsets to be populated after header parsing")
	}
}

func TestReadAllContentIntegrity(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	var allData []byte
	for i := 0; i < c.NumBlocks(); i++ {
		data, err := c.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d: %v", i, err)
			continue
		}
		allData = append(allData, data...)
	}

	expectedSize := c.UncompressedSize()
	if int64(len(allData)) != expectedSize {
		t.Errorf("Total data size mismatch: got %d, expected %d", len(allData), expectedSize)
	}
}

func TestCompressedInfoMethods(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	info := GetStat(path)
	if info == nil {
		t.Fatal("GetStat returned nil")
	}
	if info.Name() != "test.cso" {
		t.Errorf("Name() failed: got '%s'", info.Name())
	}
	size := info.Size()
	if size <= 0 {
		t.Errorf("Size() failed: got %d", size)
	}
	if info.IsDir() {
		t.Error("IsDir() should return false")
	}
}

func TestLastBlockSize(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	lastData, err := c.ReadBlock(c.NumBlocks() - 1)
	if err != nil {
		t.Errorf("Failed to read last block: %v", err)
		return
	}

	blockSize := c.BlockSize()
	if len(lastData) > blockSize {
		t.Errorf("Last block size %d exceeds blockSize %d", len(lastData), blockSize)
	}
}

func TestRandomBlockAccess(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	numBlocks := c.NumBlocks()
	blockSizes := make(map[int]int)

	for i := numBlocks - 1; i >= 0; i-- {
		data, err := c.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d (reverse): %v", i, err)
			continue
		}
		blockSizes[i] = len(data)
	}

	for i := 0; i < numBlocks; i++ {
		data, err := c.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d (forward): %v", i, err)
			continue
		}
		if blockSizes[i] != len(data) {
			t.Errorf("Block %d size inconsistent: reverse=%d, forward=%d", i, blockSizes[i], len(data))
		}
	}
}

func TestInvalidMagicError(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "invalid-cso-*.bin")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	invalidMagic := []byte{'X', 'Y', 'Z', 'W'}
	tmpFile.Write(invalidMagic)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	_, err = Open(tmpFile.Name())
	if err == nil {
		t.Error("Expected error for invalid magic bytes")
	}
}

func TestConcurrentAccessBasic(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c1, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}

	c2, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open second instance of CSO file: %v", err)
	}

	defer c1.Close()
	defer c2.Close()

	data1, err := c1.ReadBlock(0)
	if err != nil {
		t.Errorf("Failed to read from first instance: %v", err)
	}

	data2, err := c2.ReadBlock(0)
	if err != nil {
		t.Errorf("Failed to read from second instance: %v", err)
	}

	if !bytes.Equal(data1, data2) {
		t.Error("Concurrent instances returned different data for same block")
	}
}

func TestGetStatBothFormats(t *testing.T) {
	csoInfo := GetStat(filepath.Join(testDir, "test.ciso"))
	if csoInfo != nil && csoInfo.Name() != "test.ciso" {
		t.Errorf("Expected name 'test.ciso', got '%s'", csoInfo.Name())
	}

	csoInfo = GetStat(filepath.Join(testDir, "test.cso"))
	if csoInfo == nil || csoInfo.Name() != "test.cso" {
		t.Error("GetStat failed for CSO file")
	}
}

func TestCompressedFlagHandling(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	for i := 0; i < c.NumBlocks(); i++ {
		data, err := c.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d: %v", i, err)
			continue
		}
		_ = data
	}
}

func TestAllBlocksComplete(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	totalExpectedSize := int64(0)
	for i := 0; i < c.NumBlocks(); i++ {
		data, err := c.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d: %v", i, err)
			continue
		}
		totalExpectedSize += int64(len(data))
	}

	expected := c.UncompressedSize()
	if totalExpectedSize != expected {
		t.Errorf("Total bytes from all blocks (%d) doesn't match expected size (%d)", totalExpectedSize, expected)
	}
}

func TestBlockOffsetAlignment(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	align := uint8(c.align)
	for i := 0; i < c.NumBlocks(); i++ {
		offset := int64(c.offsets[i]&0x7FFFFFFF) << align

		if offset < 24 {
			t.Errorf("Block %d offset %d is too small (should be after header)", i, offset)
		}

		if align > 0 && offset%(1<<align) != 0 {
			t.Errorf("Block %d offset %d not aligned to 2^%d", i, offset, align)
		}
	}
}

func TestHeaderSize(t *testing.T) {
	path := filepath.Join(testDir, "test.cso")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CSO file: %v", err)
	}
	defer c.Close()

	firstOffset := int64(c.offsets[0]&0x7FFFFFFF) << c.align
	if firstOffset < 24 {
		t.Errorf("First block offset %d should be >= header size (24)", firstOffset)
	}
}

func TestGetStatNonCSOFile(t *testing.T) {
	info := GetStat(filepath.Join(testDir, "test.zso"))
	if info != nil {
		t.Error("Expected nil for non-CSO file (ZSO)")
	}

	info = GetStat(filepath.Join(testDir, "test.iso"))
	if info != nil {
		t.Error("Expected nil for regular ISO file")
	}
}
