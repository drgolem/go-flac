package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"

	"github.com/drgolem/go-flac/flac"
)

func main() {
	slog.Info("RAW to FLAC encoder")

	if len(os.Args) < 6 {
		fmt.Fprintln(os.Stderr, "usage: raw2flac <infile.raw> <outfile.flac> <sample_rate> <channels> <bits_per_sample>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Encodes raw interleaved little-endian PCM to FLAC.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "example:")
		fmt.Fprintln(os.Stderr, "  # First decode a FLAC to raw PCM:")
		fmt.Fprintln(os.Stderr, "  go run ./examples/flac2raw input.flac output.raw")
		fmt.Fprintln(os.Stderr, "  # Then encode back to FLAC:")
		fmt.Fprintln(os.Stderr, "  go run ./examples/raw2flac output.raw re-encoded.flac 44100 2 16")
		return
	}

	inFile := os.Args[1]
	outFile := os.Args[2]
	sampleRate, _ := strconv.Atoi(os.Args[3])
	channels, _ := strconv.Atoi(os.Args[4])
	bitsPerSample, _ := strconv.Atoi(os.Args[5])

	slog.Info("Processing files",
		"input", inFile, "output", outFile,
		"rate", sampleRate, "channels", channels, "bps", bitsPerSample)

	slog.Info("libFLAC version", "version", flac.GetVersion())

	// Open input raw PCM file
	fIn, err := os.Open(inFile)
	if err != nil {
		slog.Error("Failed to open input file", "error", err)
		return
	}
	defer fIn.Close()

	// Calculate total samples from file size
	stat, err := fIn.Stat()
	if err != nil {
		slog.Error("Failed to stat input file", "error", err)
		return
	}
	bytesPerSample := bitsPerSample / 8
	totalSamples := stat.Size() / int64(channels*bytesPerSample)
	slog.Info("Input file", "size", stat.Size(), "totalSamples", totalSamples)

	// Create encoder
	enc, err := flac.NewFlacEncoder(sampleRate, channels, bitsPerSample)
	if err != nil {
		slog.Error("Failed to create encoder", "error", err)
		return
	}
	defer enc.Close()

	if err := enc.SetTotalSamplesEstimate(totalSamples); err != nil {
		slog.Warn("Failed to set total samples estimate", "error", err)
	}

	if err := enc.InitFile(outFile); err != nil {
		slog.Error("Failed to init encoder", "error", err)
		return
	}

	// Read and encode in chunks
	const samplesPerChunk = 4096
	pcmBuf := make([]byte, samplesPerChunk*channels*bytesPerSample)
	int32Buf := make([]int32, samplesPerChunk*channels)
	totalEncoded := int64(0)

	for {
		n, err := io.ReadFull(fIn, pcmBuf)
		if n == 0 {
			break
		}

		numSamples := n / (channels * bytesPerSample)
		usableBytes := numSamples * channels * bytesPerSample

		count := flac.PCMToInt32(pcmBuf[:usableBytes], bitsPerSample, int32Buf)
		samplesPerChannel := count / channels

		if err := enc.ProcessInterleaved(int32Buf[:count], samplesPerChannel); err != nil {
			slog.Error("Failed to encode", "error", err)
			return
		}
		totalEncoded += int64(samplesPerChannel)

		if err != nil {
			break // EOF or read error
		}
	}

	if err := enc.Finish(); err != nil {
		slog.Error("Failed to finish encoding", "error", err)
		return
	}

	slog.Info("Encoding complete", "samplesEncoded", totalEncoded)
}
