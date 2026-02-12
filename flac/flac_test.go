package flac

import (
	"io"
	"testing"
)

func TestNewFlacFrameDecoder_ValidBitDepths(t *testing.T) {
	validDepths := []int{8, 16, 24, 32}

	for _, depth := range validDepths {
		dec, err := NewFlacFrameDecoder(depth)
		if err != nil {
			t.Errorf("NewFlacFrameDecoder(%d) failed: %v", depth, err)
		}
		if dec == nil {
			t.Errorf("NewFlacFrameDecoder(%d) returned nil decoder", depth)
		}
		if dec != nil {
			dec.Delete()
		}
	}
}

func TestNewFlacFrameDecoder_InvalidBitDepths(t *testing.T) {
	invalidDepths := []int{0, 1, 7, 12, 15, 20, 48, 64}

	for _, depth := range invalidDepths {
		dec, err := NewFlacFrameDecoder(depth)
		if err == nil {
			t.Errorf("NewFlacFrameDecoder(%d) should have failed but succeeded", depth)
			if dec != nil {
				dec.Delete()
			}
		}
		if dec != nil {
			t.Errorf("NewFlacFrameDecoder(%d) should return nil decoder on error", depth)
		}
	}
}

func TestDecodeSamples_Validation(t *testing.T) {
	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	// Initialize some fields that would normally be set by Open()
	dec.channels = 2
	dec.outputBytesPerSample = 2

	audio := make([]byte, 1024)

	// Test negative samples
	_, err = dec.DecodeSamples(-1, audio)
	if err == nil {
		t.Error("DecodeSamples should fail with negative samples")
	}

	// Test zero samples
	_, err = dec.DecodeSamples(0, audio)
	if err == nil {
		t.Error("DecodeSamples should fail with zero samples")
	}
}

func TestDecodeSamples_BufferSizeValidation(t *testing.T) {
	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	// Initialize fields
	dec.channels = 2
	dec.outputBytesPerSample = 2

	// Request 100 samples = 100 * 2 channels * 2 bytes = 400 bytes needed
	// But provide buffer that's too small
	audio := make([]byte, 100) // Only 100 bytes

	_, err = dec.DecodeSamples(100, audio)
	if err == nil {
		t.Error("DecodeSamples should fail when buffer is too small")
	}
}

func TestDecodeSamples_OverflowCheck(t *testing.T) {
	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	// Initialize fields
	dec.channels = 2
	dec.outputBytesPerSample = 2

	// Try to decode an absurdly large number of samples that would overflow
	audio := make([]byte, 1024)
	const maxInt = int(^uint(0) >> 1)

	_, err = dec.DecodeSamples(maxInt, audio)
	if err == nil {
		t.Error("DecodeSamples should fail with overflow-inducing sample count")
	}
}

func TestSeek_Validation(t *testing.T) {
	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	// Set up some state
	dec.totalSamples = 1000
	dec.currentSample = 100

	// Test seeking to negative position
	_, err = dec.Seek(-10, io.SeekStart)
	if err == nil {
		t.Error("Seek should fail when seeking to negative position")
	}

	// Test seeking beyond end
	_, err = dec.Seek(2000, io.SeekStart)
	if err == nil {
		t.Error("Seek should fail when seeking beyond end of stream")
	}

	// Test SeekEnd support
	_, err = dec.Seek(-50, io.SeekEnd)
	// This should attempt to seek to position 950 (1000 - 50)
	// It might fail because decoder isn't actually open, but it shouldn't panic
}

func TestInt32toInt24LEBytes(t *testing.T) {
	tests := []struct {
		input    int32
		expected [3]byte
	}{
		{0, [3]byte{0, 0, 0}},
		{1, [3]byte{1, 0, 0}},
		{256, [3]byte{0, 1, 0}},
		{65536, [3]byte{0, 0, 1}},
		{-1, [3]byte{0xFF, 0xFF, 0xFF}},
		{0x123456, [3]byte{0x56, 0x34, 0x12}},
	}

	for _, tt := range tests {
		var result [3]byte
		int32toInt24LEBytes(tt.input, &result)
		if result != tt.expected {
			t.Errorf("int32toInt24LEBytes(%d) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestGetVersion(t *testing.T) {
	version := GetVersion()
	if version == "" {
		t.Error("GetVersion should return non-empty string")
	}
	t.Logf("libFLAC version: %s", version)
}

func TestFlacDecoder_CloseWithoutOpen(t *testing.T) {
	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}

	// Close without opening should not panic
	err = dec.Close()
	if err != nil {
		t.Errorf("Close should succeed even without Open: %v", err)
	}

	dec.Delete()
}

// Additional file-based tests

func TestFlacDecoder_OpenAndDecode(t *testing.T) {
	testFile := "../examples/test_1.flac"

	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	err = dec.Open(testFile)
	if err != nil {
		t.Skipf("Test file not available: %v", err)
		return
	}
	defer dec.Close()

	// Verify format information
	rate, channels, bitsPerSample := dec.GetFormat()
	if rate <= 0 {
		t.Errorf("Invalid sample rate: %d", rate)
	}
	if channels <= 0 {
		t.Errorf("Invalid channel count: %d", channels)
	}
	if bitsPerSample <= 0 {
		t.Errorf("Invalid bits per sample: %d", bitsPerSample)
	}

	// Decode some samples
	audioSamples := 4096
	audioBufferBytes := audioSamples * channels * 2 // 16-bit output
	audio := make([]byte, audioBufferBytes)

	sampleCnt, err := dec.DecodeSamples(audioSamples, audio)
	if err != nil && err.Error() != "EOF" {
		t.Errorf("DecodeSamples failed: %v", err)
	}
	if sampleCnt <= 0 {
		t.Errorf("No samples decoded: %d", sampleCnt)
	}
}

func TestFlacDecoder_GetFormatWithFile(t *testing.T) {
	testFile := "../examples/test_1.flac"

	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	err = dec.Open(testFile)
	if err != nil {
		t.Skipf("Test file not available: %v", err)
		return
	}
	defer dec.Close()

	rate, channels, bitsPerSample := dec.GetFormat()

	// Verify reasonable values
	if rate < 8000 || rate > 192000 {
		t.Errorf("Unexpected sample rate: %d", rate)
	}
	if channels < 1 || channels > 8 {
		t.Errorf("Unexpected channel count: %d", channels)
	}
	if bitsPerSample != 8 && bitsPerSample != 16 && bitsPerSample != 24 && bitsPerSample != 32 {
		t.Errorf("Unexpected bits per sample: %d", bitsPerSample)
	}
}

func TestFlacDecoder_TotalSamplesWithFile(t *testing.T) {
	testFile := "../examples/test_1.flac"

	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	err = dec.Open(testFile)
	if err != nil {
		t.Skipf("Test file not available: %v", err)
		return
	}
	defer dec.Close()

	totalSamples := dec.TotalSamples()
	if totalSamples <= 0 {
		t.Errorf("Invalid total samples: %d", totalSamples)
	}

	currentSample := dec.TellCurrentSample()
	if currentSample != 0 {
		t.Errorf("Expected current sample to be 0, got %d", currentSample)
	}
}

func TestFlacDecoder_SeekWithFile(t *testing.T) {
	testFile := "../examples/test_1.flac"

	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	err = dec.Open(testFile)
	if err != nil {
		t.Skipf("Test file not available: %v", err)
		return
	}
	defer dec.Close()

	totalSamples := dec.TotalSamples()
	if totalSamples <= 0 {
		t.Skip("Cannot test seek with invalid total samples")
		return
	}

	// Test seeking to middle
	targetSample := totalSamples / 2
	pos, err := dec.Seek(targetSample, 0) // io.SeekStart
	if err != nil {
		t.Errorf("Seek to middle failed: %v", err)
	}
	if pos != targetSample {
		t.Errorf("Seek returned wrong position: expected %d, got %d", targetSample, pos)
	}

	// Verify current position
	currentSample := dec.TellCurrentSample()
	if currentSample != targetSample {
		t.Errorf("Current sample after seek: expected %d, got %d", targetSample, currentSample)
	}

	// Test seeking to start
	pos, err = dec.Seek(0, 0) // io.SeekStart
	if err != nil {
		t.Errorf("Seek to start failed: %v", err)
	}
	if pos != 0 {
		t.Errorf("Seek to start returned wrong position: %d", pos)
	}
}

func TestFlacDecoder_SeekInvalidPositions(t *testing.T) {
	testFile := "../examples/test_1.flac"

	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	err = dec.Open(testFile)
	if err != nil {
		t.Skipf("Test file not available: %v", err)
		return
	}
	defer dec.Close()

	// Test seeking to negative position
	_, err = dec.Seek(-1, 0)
	if err == nil {
		t.Error("Seek to negative position should fail")
	}

	// Test seeking beyond total samples
	totalSamples := dec.TotalSamples()
	_, err = dec.Seek(totalSamples+1000, 0)
	if err == nil {
		t.Error("Seek beyond total samples should fail")
	}
}

func TestFlacDecoder_DecodeSamplesWithFile(t *testing.T) {
	testFile := "../examples/test_1.flac"

	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	err = dec.Open(testFile)
	if err != nil {
		t.Skipf("Test file not available: %v", err)
		return
	}
	defer dec.Close()

	_, channels, _ := dec.GetFormat()

	// Test with zero samples
	audio := make([]byte, 4096)
	_, err = dec.DecodeSamples(0, audio)
	if err == nil {
		t.Error("DecodeSamples with 0 samples should fail")
	}

	// Test with buffer too small
	_, err = dec.DecodeSamples(4096, make([]byte, 10))
	if err == nil {
		t.Error("DecodeSamples with small buffer should fail")
	}

	// Test with valid parameters
	audioSamples := 4096
	audioBufferBytes := audioSamples * channels * 2
	audio = make([]byte, audioBufferBytes)
	sampleCnt, err := dec.DecodeSamples(audioSamples, audio)
	if err != nil && err.Error() != "EOF" {
		t.Errorf("DecodeSamples with valid parameters failed: %v", err)
	}
	if sampleCnt < 0 || sampleCnt > audioSamples {
		t.Errorf("Invalid sample count returned: %d", sampleCnt)
	}
}

func TestFlacDecoder_MultipleDecodeCycles(t *testing.T) {
	testFile := "../examples/test_1.flac"

	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	err = dec.Open(testFile)
	if err != nil {
		t.Skipf("Test file not available: %v", err)
		return
	}
	defer dec.Close()

	_, channels, _ := dec.GetFormat()
	audioSamples := 1024
	audioBufferBytes := audioSamples * channels * 2
	audio := make([]byte, audioBufferBytes)

	totalDecoded := 0
	maxIterations := 10

	for i := 0; i < maxIterations; i++ {
		sampleCnt, err := dec.DecodeSamples(audioSamples, audio)
		if err != nil {
			break // EOF or error
		}
		if sampleCnt == 0 {
			break
		}
		totalDecoded += sampleCnt
	}

	if totalDecoded == 0 {
		t.Error("No samples decoded in multiple cycles")
	}
}

func TestFlacDecoder_GetResolvedStateWithFile(t *testing.T) {
	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	state := dec.GetResolvedState()
	if state == "" {
		t.Error("GetResolvedState returned empty string")
	}

	// After open
	testFile := "../examples/test_1.flac"
	err = dec.Open(testFile)
	if err != nil {
		t.Skipf("Test file not available: %v", err)
		return
	}
	defer dec.Close()

	state = dec.GetResolvedState()
	if state == "" {
		t.Error("GetResolvedState returned empty string after open")
	}
}

func TestFlacDecoder_OpenNonExistentFile(t *testing.T) {
	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	err = dec.Open("/nonexistent/file.flac")
	if err == nil {
		t.Error("Opening nonexistent file should fail")
		dec.Close()
	}
}

func TestFlacDecoder_DoubleClose(t *testing.T) {
	testFile := "../examples/test_1.flac"

	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	err = dec.Open(testFile)
	if err != nil {
		t.Skipf("Test file not available: %v", err)
		return
	}

	// First close
	err = dec.Close()
	if err != nil {
		t.Errorf("First close failed: %v", err)
	}

	// Second close should not panic
	err = dec.Close()
	if err != nil {
		t.Errorf("Second close failed: %v", err)
	}
}
