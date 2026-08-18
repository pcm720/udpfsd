//go:build cgo && !nochd

package chd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const testDir = "../test/"

func TestOpenValidCHD(t *testing.T) {
	path := filepath.Join(testDir, "test_dvd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
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
	if info.Name() != "test_dvd.chd" {
		t.Errorf("Expected name 'test_dvd.chd', got '%s'", info.Name())
	}
}

func TestOpenCDFormat(t *testing.T) {
	path := filepath.Join(testDir, "test_cd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
	}
	defer c.Close()

	if !c.isCDFormat {
		t.Error("Expected test_cd.chd to be recognized as CD format")
	}

	info := c.Stat()
	if info == nil {
		t.Fatal("Stat() returned nil")
	}
	if info.Name() != "test_cd.chd" {
		t.Errorf("Expected name 'test_cd.chd', got '%s'", info.Name())
	}
}

func TestReadBlocksDVD(t *testing.T) {
	path := filepath.Join(testDir, "test_dvd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
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
		for i := range min(len(ref), len(decompressedData)) {
			if ref[i] != decompressedData[i] {
				t.Logf("First difference at byte %d: expected 0x%02X, got 0x%02X", i, ref[i], decompressedData[i])
				break
			}
		}
	}
}

func TestReadBlocksCD(t *testing.T) {
	path := filepath.Join(testDir, "test_cd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
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
	for i := range min(len(ref), len(decompressedData)) {
		if ref[i] != decompressedData[i] {
			t.Logf("First difference at byte %d: expected 0x%02X, got 0x%02X", i, ref[i], decompressedData[i])
			t.Errorf("Decompressed CD file does not match the reference ISO")
			break
		}
	}
}

func TestReadBlockInvalidIndex(t *testing.T) {
	path := filepath.Join(testDir, "test_dvd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
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

func TestGetStatValidCHD(t *testing.T) {
	path := filepath.Join(testDir, "test_dvd.chd")
	info := GetStat(path)
	if info == nil {
		t.Fatal("GetStat returned nil for valid CHD file")
	}
	if info.Name() != "test_dvd.chd" {
		t.Errorf("Expected name 'test_dvd.chd', got '%s'", info.Name())
	}
	if info.Size() <= 0 {
		t.Errorf("Expected size > 0, got %d", info.Size())
	}
	if info.IsDir() {
		t.Error("GetStat should return non-directory file")
	}
}

func TestGetStatCDFormat(t *testing.T) {
	path := filepath.Join(testDir, "test_cd.chd")
	info := GetStat(path)
	if info == nil {
		t.Fatal("GetStat returned nil for valid CHD CD file")
	}
	if info.Name() != "test_cd.chd" {
		t.Errorf("Expected name 'test_cd.chd', got '%s'", info.Name())
	}
	if info.Size() <= 0 {
		t.Errorf("Expected size > 0, got %d", info.Size())
	}
}

func TestGetStatInvalidFile(t *testing.T) {
	info := GetStat(filepath.Join(testDir, "nonexistent.chd"))
	if info != nil {
		t.Error("Expected nil for nonexistent file")
	}

	info = GetStat(filepath.Join(testDir, "test.cso"))
	if info != nil {
		t.Error("Expected nil for non-CHD file (CSO)")
	}

	info = GetStat(filepath.Join(testDir, "test.iso"))
	if info != nil {
		t.Error("Expected nil for ISO file (not CHD)")
	}
}

func TestOpenNonExistentFile(t *testing.T) {
	_, err := Open("/nonexistent/path/to/file.chd")
	if err == nil {
		t.Error("Expected error when opening nonexistent file")
	}
}

func TestCloseMultipleTimes(t *testing.T) {
	path := filepath.Join(testDir, "test_dvd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
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
	path := filepath.Join(testDir, "test_dvd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
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
	path := filepath.Join(testDir, "test_dvd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
	}
	defer c.Close()

	if c.NumBlocks() == 0 {
		t.Error("Expected numBlocks > 0 after header parsing")
	}
}

func TestCompressedInfoMethods(t *testing.T) {
	path := filepath.Join(testDir, "test_dvd.chd")
	info := GetStat(path)
	if info == nil {
		t.Fatal("GetStat returned nil")
	}
	if info.Name() != "test_dvd.chd" {
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
	path := filepath.Join(testDir, "test_dvd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
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
	path := filepath.Join(testDir, "test_dvd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
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

func TestConcurrentAccess(t *testing.T) {
	path := filepath.Join(testDir, "test_dvd.chd")
	c1, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
	}

	c2, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open second instance of CHD file: %v", err)
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

func TestCDFormatDetectionDVD(t *testing.T) {
	path := filepath.Join(testDir, "test_dvd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
	}
	defer c.Close()

	if c.isCDFormat {
		t.Error("Expected DVD format file not to be detected as CD format")
	}
}

func TestCDFormatDetectionCD(t *testing.T) {
	path := filepath.Join(testDir, "test_cd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
	}
	defer c.Close()

	if !c.isCDFormat {
		t.Error("Expected CD format file to be detected as CD format")
	}

	if c.framesPerHunk <= 0 {
		t.Errorf("Expected framesPerHunk > 0 for CD format, got %d", c.framesPerHunk)
	}
}

func TestGetStatNonCHDFile(t *testing.T) {
	info := GetStat(filepath.Join(testDir, "test.cso"))
	if info != nil {
		t.Error("Expected nil for non-CHD file (CSO)")
	}

	info = GetStat(filepath.Join(testDir, "test.iso"))
	if info != nil {
		t.Error("Expected nil for regular ISO file")
	}
}

func TestAllBlocksCompleteDVD(t *testing.T) {
	path := filepath.Join(testDir, "test_dvd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
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

func TestAllBlocksCompleteCD(t *testing.T) {
	path := filepath.Join(testDir, "test_cd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
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

func TestReadBlockDVDSizeMatchesExpected(t *testing.T) {
	path := filepath.Join(testDir, "test_dvd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
	}
	defer c.Close()

	expectedBlockSize := c.BlockSize()
	for i := 0; i < c.NumBlocks(); i++ {
		data, err := c.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d: %v", i, err)
			continue
		}
		if len(data) != expectedBlockSize && i < c.NumBlocks()-1 {
			t.Errorf("Block %d size %d doesn't match expected blockSize %d", i, len(data), expectedBlockSize)
		}
	}
}

func TestReadBlockCDSizeMatchesExpected(t *testing.T) {
	path := filepath.Join(testDir, "test_cd.chd")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open CHD file: %v", err)
	}
	defer c.Close()

	expectedBlockSize := c.BlockSize()
	for i := 0; i < c.NumBlocks(); i++ {
		data, err := c.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d: %v", i, err)
			continue
		}
		if len(data) != expectedBlockSize && i < c.NumBlocks()-1 {
			t.Errorf("Block %d size %d doesn't match expected blockSize %d", i, len(data), expectedBlockSize)
		}
	}
}
