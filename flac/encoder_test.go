package flac

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewFlacEncoder_ValidParams(t *testing.T) {
	tests := []struct {
		rate, channels, bps int
	}{
		{44100, 2, 16},
		{48000, 2, 24},
		{96000, 2, 24},
		{44100, 1, 16},
		{22050, 1, 8},
		{192000, 2, 32},
		{44100, 6, 16}, // 5.1
	}

	for _, tt := range tests {
		enc, err := NewFlacEncoder(tt.rate, tt.channels, tt.bps)
		if err != nil {
			t.Errorf("NewFlacEncoder(%d, %d, %d) failed: %v", tt.rate, tt.channels, tt.bps, err)
			continue
		}
		if enc == nil {
			t.Errorf("NewFlacEncoder(%d, %d, %d) returned nil", tt.rate, tt.channels, tt.bps)
			continue
		}
		enc.Close()
	}
}

func TestNewFlacEncoder_InvalidParams(t *testing.T) {
	tests := []struct {
		rate, channels, bps int
		desc                string
	}{
		{0, 2, 16, "zero sample rate"},
		{-1, 2, 16, "negative sample rate"},
		{44100, 0, 16, "zero channels"},
		{44100, 9, 16, "too many channels"},
		{44100, 2, 12, "invalid bit depth"},
		{44100, 2, 0, "zero bit depth"},
	}

	for _, tt := range tests {
		enc, err := NewFlacEncoder(tt.rate, tt.channels, tt.bps)
		if err == nil {
			t.Errorf("NewFlacEncoder(%s) should have failed", tt.desc)
			if enc != nil {
				enc.Close()
			}
		}
	}
}

func TestFlacEncoder_SetCompressionLevel(t *testing.T) {
	enc, err := NewFlacEncoder(44100, 2, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	// Valid levels
	for level := 0; level <= 8; level++ {
		if err := enc.SetCompressionLevel(level); err != nil {
			t.Errorf("SetCompressionLevel(%d) failed: %v", level, err)
		}
	}

	// Invalid levels
	if err := enc.SetCompressionLevel(-1); err == nil {
		t.Error("SetCompressionLevel(-1) should fail")
	}
	if err := enc.SetCompressionLevel(9); err == nil {
		t.Error("SetCompressionLevel(9) should fail")
	}
}

func TestFlacEncoder_InitFileAndEncode(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "test.flac")

	enc, err := NewFlacEncoder(44100, 2, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	if err := enc.InitFile(outFile); err != nil {
		t.Fatalf("InitFile failed: %v", err)
	}

	// Generate a sine-like test signal (1024 samples, stereo)
	numSamples := 1024
	samples := make([]int32, numSamples*2) // stereo
	for i := 0; i < numSamples; i++ {
		// Simple triangle wave within 16-bit range [-32768, 32767]
		val := int32((i % 256) * 127)
		if (i/256)%2 == 1 {
			val = 32512 - val // stays within 16-bit range
		}
		samples[i*2] = val   // left
		samples[i*2+1] = val // right
	}

	if err := enc.ProcessInterleaved(samples, numSamples); err != nil {
		t.Fatalf("ProcessInterleaved failed: %v", err)
	}

	if err := enc.Finish(); err != nil {
		t.Fatalf("Finish failed: %v", err)
	}

	// Verify output file exists and has content
	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatalf("Output file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("Output file is empty")
	}

	// Verify it's a valid FLAC file by decoding it
	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	if err := dec.Open(outFile); err != nil {
		t.Fatalf("Failed to open encoded FLAC: %v", err)
	}
	defer dec.Close()

	rate, channels, bps := dec.GetFormat()
	if rate != 44100 {
		t.Errorf("Expected rate 44100, got %d", rate)
	}
	if channels != 2 {
		t.Errorf("Expected 2 channels, got %d", channels)
	}
	if bps != 16 {
		t.Errorf("Expected 16 bps, got %d", bps)
	}
	if dec.TotalSamples() != int64(numSamples) {
		t.Errorf("Expected %d total samples, got %d", numSamples, dec.TotalSamples())
	}
}

func TestFlacEncoder_InitStreamAndEncode(t *testing.T) {
	enc, err := NewFlacEncoder(48000, 2, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	if err := enc.InitStream(); err != nil {
		t.Fatalf("InitStream failed: %v", err)
	}

	// Feed some audio
	numSamples := 4096
	samples := make([]int32, numSamples*2)
	for i := range samples {
		samples[i] = int32(i % 1000)
	}

	if err := enc.ProcessInterleaved(samples, numSamples); err != nil {
		t.Fatalf("ProcessInterleaved failed: %v", err)
	}

	// Should have some output bytes
	bytes := enc.TakeBytes()
	if len(bytes) == 0 {
		t.Error("Expected encoded bytes from stream mode")
	}

	if err := enc.Finish(); err != nil {
		t.Fatalf("Finish failed: %v", err)
	}

	// Finish may produce more bytes
	remaining := enc.TakeBytes()
	t.Logf("Stream mode produced %d + %d bytes", len(bytes), len(remaining))
}

func TestFlacEncoder_24bit(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "test24.flac")

	enc, err := NewFlacEncoder(96000, 2, 24)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	if err := enc.InitFile(outFile); err != nil {
		t.Fatalf("InitFile failed: %v", err)
	}

	numSamples := 2048
	samples := make([]int32, numSamples*2)
	for i := 0; i < numSamples; i++ {
		val := int32((i * 1000) % 8388608) // 24-bit range
		samples[i*2] = val
		samples[i*2+1] = -val
	}

	if err := enc.ProcessInterleaved(samples, numSamples); err != nil {
		t.Fatalf("ProcessInterleaved failed: %v", err)
	}

	if err := enc.Finish(); err != nil {
		t.Fatalf("Finish failed: %v", err)
	}

	// Verify via decoder
	dec, err := NewFlacFrameDecoder(24)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	if err := dec.Open(outFile); err != nil {
		t.Fatalf("Failed to open encoded FLAC: %v", err)
	}
	defer dec.Close()

	rate, channels, bps := dec.GetFormat()
	if rate != 96000 {
		t.Errorf("Expected rate 96000, got %d", rate)
	}
	if channels != 2 {
		t.Errorf("Expected 2 channels, got %d", channels)
	}
	if bps != 24 {
		t.Errorf("Expected 24 bps, got %d", bps)
	}
}

func TestFlacEncoder_ProcessBeforeInit(t *testing.T) {
	enc, err := NewFlacEncoder(44100, 2, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	samples := make([]int32, 100)
	err = enc.ProcessInterleaved(samples, 50)
	if err == nil {
		t.Error("ProcessInterleaved before Init should fail")
	}
}

func TestFlacEncoder_DoubleInit(t *testing.T) {
	tmpDir := t.TempDir()

	enc, err := NewFlacEncoder(44100, 2, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	if err := enc.InitFile(filepath.Join(tmpDir, "test1.flac")); err != nil {
		t.Fatalf("First InitFile failed: %v", err)
	}

	err = enc.InitFile(filepath.Join(tmpDir, "test2.flac"))
	if err == nil {
		t.Error("Second InitFile should fail")
	}
}

func TestFlacEncoder_ProcessInvalidSamples(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "test.flac")

	enc, err := NewFlacEncoder(44100, 2, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	if err := enc.InitFile(outFile); err != nil {
		t.Fatalf("InitFile failed: %v", err)
	}

	// Zero samples
	err = enc.ProcessInterleaved(make([]int32, 100), 0)
	if err == nil {
		t.Error("ProcessInterleaved with 0 samples should fail")
	}

	// Negative samples
	err = enc.ProcessInterleaved(make([]int32, 100), -1)
	if err == nil {
		t.Error("ProcessInterleaved with -1 samples should fail")
	}

	// Buffer too small
	err = enc.ProcessInterleaved(make([]int32, 1), 10)
	if err == nil {
		t.Error("ProcessInterleaved with small buffer should fail")
	}

	enc.Finish()
}

func TestFlacEncoder_GetFormat(t *testing.T) {
	enc, err := NewFlacEncoder(96000, 2, 24)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	rate, channels, bps := enc.GetFormat()
	if rate != 96000 {
		t.Errorf("Expected rate 96000, got %d", rate)
	}
	if channels != 2 {
		t.Errorf("Expected 2 channels, got %d", channels)
	}
	if bps != 24 {
		t.Errorf("Expected 24 bps, got %d", bps)
	}
}

func TestFlacEncoder_Mono(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "mono.flac")

	enc, err := NewFlacEncoder(22050, 1, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	if err := enc.InitFile(outFile); err != nil {
		t.Fatalf("InitFile failed: %v", err)
	}

	numSamples := 512
	samples := make([]int32, numSamples) // mono
	for i := 0; i < numSamples; i++ {
		samples[i] = int32(i * 10)
	}

	if err := enc.ProcessInterleaved(samples, numSamples); err != nil {
		t.Fatalf("ProcessInterleaved failed: %v", err)
	}

	if err := enc.Finish(); err != nil {
		t.Fatalf("Finish failed: %v", err)
	}

	// Verify
	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	if err := dec.Open(outFile); err != nil {
		t.Fatalf("Failed to open mono FLAC: %v", err)
	}
	defer dec.Close()

	_, channels, _ := dec.GetFormat()
	if channels != 1 {
		t.Errorf("Expected 1 channel, got %d", channels)
	}
}

func TestFlacEncoder_StreamInfo(t *testing.T) {
	enc, err := NewFlacEncoder(44100, 2, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	// StreamInfo before init should return nil
	si := enc.StreamInfo()
	if si != nil {
		t.Error("StreamInfo before init should return nil")
	}

	if err := enc.InitStream(); err != nil {
		t.Fatalf("InitStream failed: %v", err)
	}

	// Feed some audio
	numSamples := 4096
	samples := make([]int32, numSamples*2)
	for i := range samples {
		samples[i] = int32(i % 1000)
	}
	if err := enc.ProcessInterleaved(samples, numSamples); err != nil {
		t.Fatalf("ProcessInterleaved failed: %v", err)
	}

	// Finish to finalize STREAMINFO (includes MD5, total samples)
	if err := enc.Finish(); err != nil {
		t.Fatalf("Finish failed: %v", err)
	}

	si = enc.StreamInfo()
	if si == nil {
		t.Error("StreamInfo after Finish should not be nil")
	} else if len(si) != 34 {
		t.Errorf("StreamInfo should be 34 bytes, got %d", len(si))
	}
}

func TestFlacEncoder_TakeBytes(t *testing.T) {
	enc, err := NewFlacEncoder(44100, 2, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	if err := enc.InitStream(); err != nil {
		t.Fatalf("InitStream failed: %v", err)
	}

	// InitStream produces the FLAC header (fLaC marker + STREAMINFO)
	header := enc.TakeBytes()
	if len(header) == 0 {
		t.Error("Expected FLAC header bytes after InitStream")
	}

	// Second take with no new data should return empty
	bytes := enc.TakeBytes()
	if len(bytes) != 0 {
		t.Errorf("Expected empty bytes on second take, got %d bytes", len(bytes))
	}

	enc.Finish()
}

func TestFlacEncoder_SetTotalSamplesEstimate(t *testing.T) {
	enc, err := NewFlacEncoder(44100, 2, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	defer enc.Close()

	// Valid estimate before init
	if err := enc.SetTotalSamplesEstimate(100000); err != nil {
		t.Errorf("SetTotalSamplesEstimate failed: %v", err)
	}

	if err := enc.InitStream(); err != nil {
		t.Fatalf("InitStream failed: %v", err)
	}

	// After init should fail
	err = enc.SetTotalSamplesEstimate(200000)
	if err == nil {
		t.Error("SetTotalSamplesEstimate after init should fail")
	}

	enc.Finish()
}

func TestFlacEncoder_DoubleClose(t *testing.T) {
	enc, err := NewFlacEncoder(44100, 2, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}

	if err := enc.InitStream(); err != nil {
		t.Fatalf("InitStream failed: %v", err)
	}
	enc.Finish()

	// Double close should not panic
	enc.Close()
	enc.Close()
}

func TestFlacEncoder_StreamEncodeAndDecode(t *testing.T) {
	// Encode via stream mode, write bytes to file, decode and verify
	enc, err := NewFlacEncoder(44100, 2, 16)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}

	if err := enc.SetTotalSamplesEstimate(8192); err != nil {
		t.Fatalf("SetTotalSamplesEstimate failed: %v", err)
	}
	if err := enc.InitStream(); err != nil {
		t.Fatalf("InitStream failed: %v", err)
	}

	// Generate and encode test signal
	numSamples := 8192
	origSamples := generateTestSignal(numSamples, 2, 16)

	var allBytes []byte

	// Feed in chunks to simulate streaming
	chunkSize := 1024
	for fed := 0; fed < numSamples; fed += chunkSize {
		end := fed + chunkSize
		if end > numSamples {
			end = numSamples
		}
		chunk := end - fed
		start := fed * 2
		sliceEnd := start + chunk*2
		if err := enc.ProcessInterleaved(origSamples[start:sliceEnd], chunk); err != nil {
			t.Fatalf("ProcessInterleaved failed at sample %d: %v", fed, err)
		}
		bytes := enc.TakeBytes()
		allBytes = append(allBytes, bytes...)
	}

	if err := enc.Finish(); err != nil {
		t.Fatalf("Finish failed: %v", err)
	}
	allBytes = append(allBytes, enc.TakeBytes()...)
	enc.Close()

	// Write to temp file
	tmpDir := t.TempDir()
	flacFile := filepath.Join(tmpDir, "stream.flac")
	if err := os.WriteFile(flacFile, allBytes, 0644); err != nil {
		t.Fatalf("Failed to write FLAC file: %v", err)
	}

	// Decode and verify
	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	if err := dec.Open(flacFile); err != nil {
		t.Fatalf("Failed to open stream-encoded FLAC: %v", err)
	}
	defer dec.Close()

	rate, channels, bps := dec.GetFormat()
	if rate != 44100 || channels != 2 || bps != 16 {
		t.Errorf("Format mismatch: got %d/%d/%d, want 44100/2/16", rate, channels, bps)
	}

	total := dec.TotalSamples()
	if total != int64(numSamples) {
		t.Errorf("Total samples: got %d, want %d", total, numSamples)
	}

	// Decode all and compare
	pcmBuf := make([]byte, numSamples*2*2) // stereo 16-bit
	decoded := 0
	for decoded < numSamples {
		n, err := dec.DecodeSamples(numSamples-decoded, pcmBuf[decoded*4:])
		if err != nil {
			break
		}
		if n == 0 {
			break
		}
		decoded += n
	}

	if decoded != numSamples {
		t.Fatalf("Decoded %d samples, want %d", decoded, numSamples)
	}

	decodedSamples := make([]int32, numSamples*2)
	PCMToInt32(pcmBuf, 16, decodedSamples)

	mismatches := 0
	for i := 0; i < numSamples*2; i++ {
		if origSamples[i] != decodedSamples[i] {
			mismatches++
		}
	}
	if mismatches > 0 {
		t.Errorf("Stream encode round-trip: %d mismatches out of %d samples", mismatches, numSamples*2)
	}
}

func TestPCMToInt32_8bit(t *testing.T) {
	// FLAC 8-bit is signed: int8 range [-128, 127]
	pcm := []byte{
		0x00, // 0
		0x01, // 1
		0xFF, // -1 (signed)
		0x7F, // 127 (max positive)
		0x80, // -128 (min negative)
	}

	out := make([]int32, 5)
	n := PCMToInt32(pcm, 8, out)
	if n != 5 {
		t.Fatalf("Expected 5 samples, got %d", n)
	}

	expected := []int32{0, 1, -1, 127, -128}
	for i, exp := range expected {
		if out[i] != exp {
			t.Errorf("Sample %d: expected %d, got %d", i, exp, out[i])
		}
	}
}

func TestPCMToInt32_16bit(t *testing.T) {
	// Little-endian 16-bit samples: 0, 1, -1, 32767, -32768
	pcm := []byte{
		0x00, 0x00, // 0
		0x01, 0x00, // 1
		0xFF, 0xFF, // -1
		0xFF, 0x7F, // 32767
		0x00, 0x80, // -32768
	}

	out := make([]int32, 5)
	n := PCMToInt32(pcm, 16, out)
	if n != 5 {
		t.Fatalf("Expected 5 samples, got %d", n)
	}

	expected := []int32{0, 1, -1, 32767, -32768}
	for i, exp := range expected {
		if out[i] != exp {
			t.Errorf("Sample %d: expected %d, got %d", i, exp, out[i])
		}
	}
}

func TestPCMToInt32_24bit(t *testing.T) {
	pcm := []byte{
		0x00, 0x00, 0x00, // 0
		0x01, 0x00, 0x00, // 1
		0xFF, 0xFF, 0xFF, // -1
		0xFF, 0xFF, 0x7F, // 8388607 (max positive 24-bit)
		0x00, 0x00, 0x80, // -8388608 (min negative 24-bit)
	}

	out := make([]int32, 5)
	n := PCMToInt32(pcm, 24, out)
	if n != 5 {
		t.Fatalf("Expected 5 samples, got %d", n)
	}

	expected := []int32{0, 1, -1, 8388607, -8388608}
	for i, exp := range expected {
		if out[i] != exp {
			t.Errorf("Sample %d: expected %d, got %d", i, exp, out[i])
		}
	}
}

func TestPCMToInt32_32bit(t *testing.T) {
	pcm := []byte{
		0x00, 0x00, 0x00, 0x00, // 0
		0xFF, 0xFF, 0xFF, 0x7F, // max int32
		0x00, 0x00, 0x00, 0x80, // min int32
	}

	out := make([]int32, 3)
	n := PCMToInt32(pcm, 32, out)
	if n != 3 {
		t.Fatalf("Expected 3 samples, got %d", n)
	}

	expected := []int32{0, 2147483647, -2147483648}
	for i, exp := range expected {
		if out[i] != exp {
			t.Errorf("Sample %d: expected %d, got %d", i, exp, out[i])
		}
	}
}
