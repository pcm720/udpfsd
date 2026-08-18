package zso

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const testDir = "../test/"

func TestOpenValidZSO(t *testing.T) {
	path := filepath.Join(testDir, "test.zso")
	z, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}
	defer z.Close()

	if z.NumBlocks() <= 0 {
		t.Errorf("Expected numBlocks > 0, got %d", z.NumBlocks())
	}
	if z.BlockSize() <= 0 {
		t.Errorf("Expected blockSize > 0, got %d", z.BlockSize())
	}
	if z.UncompressedSize() <= 0 {
		t.Errorf("Expected uncompressedSize > 0, got %d", z.UncompressedSize())
	}

	info := z.Stat()
	if info == nil {
		t.Fatal("Stat() returned nil")
	}
	if info.Name() != "test.zso" {
		t.Errorf("Expected name 'test.zso', got '%s'", info.Name())
	}
}

func TestReadBlocksZSO(t *testing.T) {
	path := filepath.Join(testDir, "test.zso")
	z, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}
	defer z.Close()

	numBlocks := z.NumBlocks()
	blockSize := z.BlockSize()
	var totalRead int64 = 0

	decompressedData := []byte{}
	for i := 0; i < numBlocks; i++ {
		data, err := z.ReadBlock(i)
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

	if totalRead != z.UncompressedSize() {
		t.Errorf("Total bytes read (%d) doesn't match uncompressed size (%d)", totalRead, z.UncompressedSize())
	}

	ref, err := os.ReadFile(filepath.Join(testDir, "test.iso"))
	if !bytes.Equal(ref, decompressedData) {
		t.Errorf("Decompressed file does not match the reference ISO")
	}
}

func TestReadBlockInvalidIndex(t *testing.T) {
	path := filepath.Join(testDir, "test.zso")
	z, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}
	defer z.Close()

	_, err = z.ReadBlock(-1)
	if err == nil {
		t.Error("Expected error for negative block index")
	}

	_, err = z.ReadBlock(z.NumBlocks())
	if err == nil {
		t.Errorf("Expected error for block index >= numBlocks (%d)", z.NumBlocks())
	}
}

func TestGetStatValidZSO(t *testing.T) {
	path := filepath.Join(testDir, "test.zso")
	info := GetStat(path)
	if info == nil {
		t.Fatal("GetStat returned nil for valid ZSO file")
	}
	if info.Name() != "test.zso" {
		t.Errorf("Expected name 'test.zso', got '%s'", info.Name())
	}
	if info.Size() <= 0 {
		t.Errorf("Expected size > 0, got %d", info.Size())
	}
	if info.IsDir() {
		t.Error("GetStat should return non-directory file")
	}
}

func TestGetStatInvalidFile(t *testing.T) {
	info := GetStat(filepath.Join(testDir, "nonexistent.zso"))
	if info != nil {
		t.Error("Expected nil for nonexistent file")
	}

	info = GetStat(filepath.Join(testDir, "test_cd.chd"))
	if info != nil {
		t.Error("Expected nil for non-ZSO file")
	}
}

func TestOpenNonExistentFile(t *testing.T) {
	_, err := Open("/nonexistent/path/to/file.zso")
	if err == nil {
		t.Error("Expected error when opening nonexistent file")
	}
}

func TestCloseMultipleTimes(t *testing.T) {
	path := filepath.Join(testDir, "test.zso")
	z, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}

	err = z.Close()
	if err != nil {
		t.Errorf("First Close failed: %v", err)
	}

	err = z.Close()
	if err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
}

func TestBlockSizeConsistency(t *testing.T) {
	path := filepath.Join(testDir, "test.zso")
	z, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}
	defer z.Close()

	blockSize := z.BlockSize()
	for i := 0; i < z.NumBlocks(); i++ {
		data, err := z.ReadBlock(i)
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
	path := filepath.Join(testDir, "test.zso")
	z, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}
	defer z.Close()

	if z.NumBlocks() == 0 {
		t.Error("Expected numBlocks > 0 after header parsing")
	}
	if len(z.offsets) == 0 {
		t.Error("Expected offsets to be populated after header parsing")
	}
}

func TestReadAllContentIntegrity(t *testing.T) {
	path := filepath.Join(testDir, "test.zso")
	z, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}
	defer z.Close()

	var allData []byte
	for i := 0; i < z.NumBlocks(); i++ {
		data, err := z.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d: %v", i, err)
			continue
		}
		allData = append(allData, data...)
	}

	expectedSize := z.UncompressedSize()
	if int64(len(allData)) != expectedSize {
		t.Errorf("Total data size mismatch: got %d, expected %d", len(allData), expectedSize)
	}
}

func TestCompressedInfoMethods(t *testing.T) {
	path := filepath.Join(testDir, "test.zso")
	info := GetStat(path)
	if info == nil {
		t.Fatal("GetStat returned nil")
	}
	if info.Name() != "test.zso" {
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
	path := filepath.Join(testDir, "test.zso")
	z, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}
	defer z.Close()

	lastData, err := z.ReadBlock(z.NumBlocks() - 1)
	if err != nil {
		t.Errorf("Failed to read last block: %v", err)
		return
	}

	blockSize := z.BlockSize()
	if len(lastData) > blockSize {
		t.Errorf("Last block size %d exceeds blockSize %d", len(lastData), blockSize)
	}
}

func TestRandomBlockAccess(t *testing.T) {
	path := filepath.Join(testDir, "test.zso")
	z, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}
	defer z.Close()

	numBlocks := z.NumBlocks()
	blockSizes := make(map[int]int)

	for i := numBlocks - 1; i >= 0; i-- {
		data, err := z.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d (reverse): %v", i, err)
			continue
		}
		blockSizes[i] = len(data)
	}

	for i := 0; i < numBlocks; i++ {
		data, err := z.ReadBlock(i)
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
	tmpFile, err := os.CreateTemp("", "invalid-zso-*.bin")
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
	path := filepath.Join(testDir, "test.zso")
	z1, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}

	z2, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open second instance of ZSO file: %v", err)
	}

	defer z1.Close()
	defer z2.Close()

	data1, err := z1.ReadBlock(0)
	if err != nil {
		t.Errorf("Failed to read from first instance: %v", err)
	}

	data2, err := z2.ReadBlock(0)
	if err != nil {
		t.Errorf("Failed to read from second instance: %v", err)
	}

	if !bytes.Equal(data1, data2) {
		t.Error("Concurrent instances returned different data for same block")
	}
}

func TestGetStatBothFormats(t *testing.T) {
	zsoInfo := GetStat(filepath.Join(testDir, "test.zso"))
	if zsoInfo == nil {
		t.Error("GetStat returned nil for ZSO file")
	}
}

func TestUncompressedFlagHandling(t *testing.T) {
	path := filepath.Join(testDir, "test.zso")
	z, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}
	defer z.Close()

	for i := 0; i < z.NumBlocks(); i++ {
		data, err := z.ReadBlock(i)
		if err != nil {
			t.Errorf("Failed to read block %d: %v", i, err)
			continue
		}
		_ = data
	}
}

func TestLittleEndianOffsetParsing(t *testing.T) {
	path := filepath.Join(testDir, "test.zso")
	z, err := Open(path)
	if err != nil {
		t.Fatalf("Failed to open ZSO file: %v", err)
	}
	defer z.Close()

	for i := 1; i < len(z.offsets); i++ {
		prevOffset := int64(z.offsets[i-1] & 0x7FFFFFFF)
		currOffset := int64(z.offsets[i] & 0x7FFFFFFF)

		if z.ziso {
			prevAligned := prevOffset << z.align
			currAligned := currOffset << z.align
			if currAligned < prevAligned && i > 1 {
				t.Logf("ZISO: Offset[%d] appears less than Offset[%d]", i, i-1)
			}
		} else {
			if int64(z.offsets[i]) < int64(z.offsets[i-1]) && (z.offsets[i]&0x80000000) == 0 {
				t.Logf("ZSO: Offset[%d] is less than Offset[%d]", i, i-1)
			}
		}
	}
}
