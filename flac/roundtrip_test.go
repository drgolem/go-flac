package flac

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

const chunkSamples = 4096

// TestRoundtrip_SyntheticData encodes synthetic PCM to FLAC and decodes it back,
// verifying lossless round-trip for all supported bit depths.
func TestRoundtrip_SyntheticData(t *testing.T) {
	tests := []struct {
		rate, channels, bps int
	}{
		{44100, 2, 16},
		{48000, 2, 24},
		{96000, 1, 16},
		{22050, 2, 8},
	}

	for _, tt := range tests {
		t.Run(
			formatTestName(tt.rate, tt.channels, tt.bps),
			func(t *testing.T) {
				testRoundtripSynthetic(t, tt.rate, tt.channels, tt.bps)
			},
		)
	}
}

func formatTestName(rate, channels, bps int) string {
	ch := "stereo"
	if channels == 1 {
		ch = "mono"
	}
	return fmt.Sprintf("%dHz_%s_%dbit", rate, ch, bps)
}

func testRoundtripSynthetic(t *testing.T, sampleRate, channels, bps int) {
	tmpDir := t.TempDir()
	flacFile := filepath.Join(tmpDir, "roundtrip.flac")

	// Generate test signal
	numSamples := 8192
	origSamples := generateTestSignal(numSamples, channels, bps)

	// Encode to FLAC
	enc, err := NewFlacEncoder(sampleRate, channels, bps)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}

	if err := enc.SetTotalSamplesEstimate(int64(numSamples)); err != nil {
		t.Fatalf("Failed to set total samples: %v", err)
	}
	if err := enc.InitFile(flacFile); err != nil {
		t.Fatalf("InitFile failed: %v", err)
	}

	if err := enc.ProcessInterleaved(origSamples, numSamples); err != nil {
		t.Fatalf("ProcessInterleaved failed: %v", err)
	}

	if err := enc.Finish(); err != nil {
		t.Fatalf("Finish failed: %v", err)
	}
	enc.Close()

	// Decode the encoded FLAC
	dec, err := NewFlacFrameDecoder(bps)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec.Delete()

	if err := dec.Open(flacFile); err != nil {
		t.Fatalf("Failed to open encoded FLAC: %v", err)
	}
	defer dec.Close()

	// Verify format
	decRate, decCh, decBps := dec.GetFormat()
	if decRate != sampleRate {
		t.Errorf("Rate mismatch: encoded %d, decoded %d", sampleRate, decRate)
	}
	if decCh != channels {
		t.Errorf("Channels mismatch: encoded %d, decoded %d", channels, decCh)
	}
	if decBps != bps {
		t.Errorf("BPS mismatch: encoded %d, decoded %d", bps, decBps)
	}
	if dec.TotalSamples() != int64(numSamples) {
		t.Errorf("Total samples mismatch: encoded %d, decoded %d", numSamples, dec.TotalSamples())
	}

	// Decode all samples
	bytesPerSample := bps / 8
	pcmBuf := make([]byte, numSamples*channels*bytesPerSample)
	totalDecoded := 0
	for totalDecoded < numSamples {
		remaining := numSamples - totalDecoded
		offset := totalDecoded * channels * bytesPerSample
		n, err := dec.DecodeSamples(remaining, pcmBuf[offset:])
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("DecodeSamples failed: %v", err)
		}
		if n == 0 {
			break
		}
		totalDecoded += n
	}

	if totalDecoded != numSamples {
		t.Fatalf("Decoded %d samples, expected %d", totalDecoded, numSamples)
	}

	// Convert decoded PCM back to int32 and compare
	decodedSamples := make([]int32, numSamples*channels)
	PCMToInt32(pcmBuf, bps, decodedSamples)

	mismatches := 0
	for i := 0; i < numSamples*channels; i++ {
		if origSamples[i] != decodedSamples[i] {
			if mismatches < 10 {
				t.Errorf("Sample %d mismatch: original %d, decoded %d", i, origSamples[i], decodedSamples[i])
			}
			mismatches++
		}
	}
	if mismatches > 0 {
		t.Errorf("Total mismatches: %d out of %d samples", mismatches, numSamples*channels)
	}
}

// generateTestSignal creates a deterministic test signal within the valid
// range for the given bit depth.
func generateTestSignal(numSamples, channels, bps int) []int32 {
	maxVal := int32(1<<(bps-1)) - 1 // e.g., 32767 for 16-bit
	samples := make([]int32, numSamples*channels)

	for i := 0; i < numSamples; i++ {
		for ch := 0; ch < channels; ch++ {
			idx := i*channels + ch
			// Mix of patterns to exercise the encoder:
			// - Low frequency ramp
			// - High frequency alternation
			// - Silence regions
			switch {
			case i < numSamples/4:
				// Ramp up
				samples[idx] = int32(int64(i) * int64(maxVal) / int64(numSamples/4))
			case i < numSamples/2:
				// Ramp down
				samples[idx] = int32(int64(numSamples/2-i) * int64(maxVal) / int64(numSamples/4))
			case i < 3*numSamples/4:
				// High frequency (alternating sign)
				if i%2 == 0 {
					samples[idx] = maxVal / 4
				} else {
					samples[idx] = -(maxVal / 4)
				}
			default:
				// Near-silence with small values
				samples[idx] = int32(i%7 - 3)
			}

			// Offset channels slightly for stereo decorrelation testing
			if ch > 0 {
				samples[idx] = samples[idx] / 2
			}
		}
	}
	return samples
}

// TestRoundtrip_FlacFile tests end-to-end: decode existing FLAC → raw PCM → encode to FLAC → decode → compare.
// Uses test_96_24.flac if available (set FLAC_TEST_FILE env var).
func TestRoundtrip_FlacFile(t *testing.T) {
	testFile := os.Getenv("FLAC_TEST_FILE")
	if testFile == "" {
		// Try default locations
		candidates := []string{
			"../examples/test_1.flac",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				testFile = c
				break
			}
		}
	}
	if testFile == "" {
		t.Skip("No test FLAC file available (set FLAC_TEST_FILE)")
		return
	}

	t.Logf("Using test file: %s", testFile)

	// Step 1: Decode the original FLAC to raw PCM (at native bit depth)
	dec1, err := NewFlacFrameDecoder(32) // request max depth, will output at native
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	defer dec1.Delete()

	if err := dec1.Open(testFile); err != nil {
		t.Fatalf("Failed to open test file: %v", err)
	}

	origRate, origChannels, origBps := dec1.GetFormat()
	origTotal := dec1.TotalSamples()
	t.Logf("Original: %dHz, %dch, %dbit, %d samples", origRate, origChannels, origBps, origTotal)

	// Decode all samples to PCM
	bytesPerSample := origBps / 8
	bytesPerFrame := origChannels * bytesPerSample
	pcmBuf := make([]byte, (int(origTotal)+chunkSamples)*bytesPerFrame)
	totalDecoded := 0
	for {
		offset := totalDecoded * bytesPerFrame
		remainingBytes := len(pcmBuf) - offset
		maxSamples := remainingBytes / bytesPerFrame
		if maxSamples <= 0 {
			break
		}
		if maxSamples > chunkSamples {
			maxSamples = chunkSamples
		}
		n, err := dec1.DecodeSamples(maxSamples, pcmBuf[offset:])
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Decode failed at sample %d: %v", totalDecoded, err)
		}
		if n == 0 {
			break
		}
		totalDecoded += n
	}
	dec1.Close()

	t.Logf("Decoded %d samples from original", totalDecoded)

	// Convert to int32 for the encoder
	pcmBytes := totalDecoded * origChannels * bytesPerSample
	origInt32 := make([]int32, totalDecoded*origChannels)
	PCMToInt32(pcmBuf[:pcmBytes], origBps, origInt32)

	// Step 2: Encode to a new FLAC file
	tmpDir := t.TempDir()
	reencoded := filepath.Join(tmpDir, "reencoded.flac")

	enc, err := NewFlacEncoder(origRate, origChannels, origBps)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}

	if err := enc.SetTotalSamplesEstimate(int64(totalDecoded)); err != nil {
		t.Logf("Warning: failed to set total samples estimate: %v", err)
	}
	if err := enc.InitFile(reencoded); err != nil {
		t.Fatalf("InitFile failed: %v", err)
	}

	// Feed in chunks
	fed := 0
	for fed < totalDecoded {
		chunk := chunkSamples
		if fed+chunk > totalDecoded {
			chunk = totalDecoded - fed
		}
		start := fed * origChannels
		end := start + chunk*origChannels
		if err := enc.ProcessInterleaved(origInt32[start:end], chunk); err != nil {
			t.Fatalf("ProcessInterleaved failed at sample %d: %v", fed, err)
		}
		fed += chunk
	}

	if err := enc.Finish(); err != nil {
		t.Fatalf("Finish failed: %v", err)
	}
	enc.Close()

	// Step 3: Decode the re-encoded file
	dec2, err := NewFlacFrameDecoder(32)
	if err != nil {
		t.Fatalf("Failed to create second decoder: %v", err)
	}
	defer dec2.Delete()

	if err := dec2.Open(reencoded); err != nil {
		t.Fatalf("Failed to open re-encoded file: %v", err)
	}
	defer dec2.Close()

	newRate, newChannels, newBps := dec2.GetFormat()
	if newRate != origRate || newChannels != origChannels || newBps != origBps {
		t.Errorf("Format mismatch: original %d/%d/%d, re-encoded %d/%d/%d",
			origRate, origChannels, origBps, newRate, newChannels, newBps)
	}

	newTotal := dec2.TotalSamples()
	if newTotal != int64(totalDecoded) {
		t.Errorf("Total samples mismatch: original %d, re-encoded %d", totalDecoded, newTotal)
	}

	// Decode re-encoded file
	newBytesPerFrame := newChannels * bytesPerSample
	pcmBuf2 := make([]byte, (int(newTotal)+chunkSamples)*newBytesPerFrame)
	totalDecoded2 := 0
	for {
		offset := totalDecoded2 * newBytesPerFrame
		remainingBytes := len(pcmBuf2) - offset
		maxSamples := remainingBytes / newBytesPerFrame
		if maxSamples <= 0 {
			break
		}
		if maxSamples > chunkSamples {
			maxSamples = chunkSamples
		}
		n, err := dec2.DecodeSamples(maxSamples, pcmBuf2[offset:])
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Decode re-encoded failed at sample %d: %v", totalDecoded2, err)
		}
		if n == 0 {
			break
		}
		totalDecoded2 += n
	}

	if totalDecoded2 != totalDecoded {
		t.Fatalf("Sample count mismatch: original %d, re-encoded %d", totalDecoded, totalDecoded2)
	}

	// Step 4: Compare PCM data byte-by-byte
	pcmBytes2 := totalDecoded2 * newChannels * bytesPerSample
	mismatches := 0
	for i := 0; i < pcmBytes && i < pcmBytes2; i++ {
		if pcmBuf[i] != pcmBuf2[i] {
			if mismatches < 10 {
				sampleIdx := i / bytesPerSample
				t.Errorf("Byte %d (sample %d) mismatch: original 0x%02x, re-encoded 0x%02x",
					i, sampleIdx, pcmBuf[i], pcmBuf2[i])
			}
			mismatches++
		}
	}

	if mismatches > 0 {
		t.Errorf("Total byte mismatches: %d out of %d bytes (%d samples)",
			mismatches, pcmBytes, totalDecoded*origChannels)
	} else {
		t.Logf("Round-trip PASSED: %d samples, %d bytes, 0 mismatches", totalDecoded, pcmBytes)
	}

	// Report file sizes
	origInfo, _ := os.Stat(testFile)
	reencInfo, _ := os.Stat(reencoded)
	if origInfo != nil && reencInfo != nil {
		ratio := float64(reencInfo.Size()) / float64(origInfo.Size()) * 100
		t.Logf("File sizes: original %d bytes, re-encoded %d bytes (%.1f%%)",
			origInfo.Size(), reencInfo.Size(), ratio)
	}
}

