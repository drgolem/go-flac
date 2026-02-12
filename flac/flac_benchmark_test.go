package flac

import (
	"os"
	"path/filepath"
	"testing"
)

// getBenchmarkFile returns a test FLAC file for benchmarking
// You can set FLAC_BENCHMARK_FILE env var to use a specific file
func getBenchmarkFile(b *testing.B) string {
	if file := os.Getenv("FLAC_BENCHMARK_FILE"); file != "" {
		if _, err := os.Stat(file); err == nil {
			return file
		}
	}

	// Try to find a test file in common locations
	possiblePaths := []string{
		"../examples/test_1.flac",
		"testdata/test.flac",
		"test.flac",
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	b.Skip("No FLAC test file found. Set FLAC_BENCHMARK_FILE env var or place test.flac in testdata/")
	return ""
}

// BenchmarkFlacDecoder_DecodeSamples benchmarks sample decoding performance
func BenchmarkFlacDecoder_DecodeSamples(b *testing.B) {
	testFile := getBenchmarkFile(b)

	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		b.Fatal(err)
	}
	defer dec.Delete()

	err = dec.Open(testFile)
	if err != nil {
		b.Fatal(err)
	}
	defer dec.Close()

	rate, channels, _ := dec.GetFormat()
	b.Logf("Format: %d Hz, %d channels", rate, channels)

	audioSamples := 4096
	audio := make([]byte, audioSamples*channels*2)

	b.ResetTimer()
	b.ReportAllocs()

	totalSamples := 0
	for i := 0; i < b.N; i++ {
		samples, err := dec.DecodeSamples(audioSamples, audio)
		if err != nil {
			b.Fatal(err)
		}
		if samples == 0 {
			// Reopen file for next iteration
			dec.Close()
			dec.Open(testFile)
		}
		totalSamples += samples
	}

	b.StopTimer()
	b.ReportMetric(float64(totalSamples)/b.Elapsed().Seconds()/1000000, "Msamples/sec")
}

// BenchmarkFlacDecoder_FullDecode benchmarks full file decoding performance
func BenchmarkFlacDecoder_FullDecode(b *testing.B) {
	testFile := getBenchmarkFile(b)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dec, err := NewFlacFrameDecoder(16)
		if err != nil {
			b.Fatal(err)
		}

		err = dec.Open(testFile)
		if err != nil {
			b.Fatal(err)
		}

		rate, channels, _ := dec.GetFormat()
		audioSamples := 4096
		audio := make([]byte, audioSamples*channels*2)

		totalSamples := 0
		for {
			samples, err := dec.DecodeSamples(audioSamples, audio)
			if err != nil {
				b.Fatal(err)
			}
			if samples == 0 {
				break
			}
			totalSamples += samples
		}

		dec.Close()
		dec.Delete()

		if i == 0 {
			b.ReportMetric(float64(totalSamples)/float64(rate), "duration_sec")
		}
	}
}

// BenchmarkFlacDecoder_SmallReads tests with smaller buffer sizes (more overhead)
func BenchmarkFlacDecoder_SmallReads(b *testing.B) {
	testFile := getBenchmarkFile(b)

	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		b.Fatal(err)
	}
	defer dec.Delete()

	err = dec.Open(testFile)
	if err != nil {
		b.Fatal(err)
	}
	defer dec.Close()

	_, channels, _ := dec.GetFormat()

	// Small buffer - 256 samples (more calls to DecodeSamples)
	audioSamples := 256
	audio := make([]byte, audioSamples*channels*2)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		samples, err := dec.DecodeSamples(audioSamples, audio)
		if err != nil {
			b.Fatal(err)
		}
		if samples == 0 {
			dec.Close()
			dec.Open(testFile)
		}
	}
}

// BenchmarkFlacDecoder_LargeReads tests with larger buffer sizes (less overhead)
func BenchmarkFlacDecoder_LargeReads(b *testing.B) {
	testFile := getBenchmarkFile(b)

	dec, err := NewFlacFrameDecoder(16)
	if err != nil {
		b.Fatal(err)
	}
	defer dec.Delete()

	err = dec.Open(testFile)
	if err != nil {
		b.Fatal(err)
	}
	defer dec.Close()

	_, channels, _ := dec.GetFormat()

	// Large buffer - 16384 samples
	audioSamples := 16384
	audio := make([]byte, audioSamples*channels*2)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		samples, err := dec.DecodeSamples(audioSamples, audio)
		if err != nil {
			b.Fatal(err)
		}
		if samples == 0 {
			dec.Close()
			dec.Open(testFile)
		}
	}
}

// Example output formatter
func init() {
	// Set path to test file if it exists in examples
	if _, err := os.Stat("../examples/test_1.flac"); err == nil {
		absPath, _ := filepath.Abs("../examples/test_1.flac")
		os.Setenv("FLAC_BENCHMARK_FILE", absPath)
	}
}
