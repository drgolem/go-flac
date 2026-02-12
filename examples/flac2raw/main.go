package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/drgolem/go-flac/flac"
)

func main() {
	slog.Info("FLAC to RAW converter")

	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: flac2raw <infile.flac> <outfile.raw>")
		fmt.Fprintln(os.Stderr, "play: ffplay  -f s16le -ar 44100 -ch_layout stereo <outfile.raw>")
		return
	}

	inFile := os.Args[1]
	outFile := os.Args[2]
	slog.Info("Processing files", "input", inFile, "output", outFile)

	slog.Info("libFLAC version", "version", flac.GetVersion())

	outBitsPerSample := 16
	outBytesPerSample := outBitsPerSample / 8

	dec, err := flac.NewFlacFrameDecoder(outBitsPerSample)
	if err != nil {
		panic(err)
	}
	defer dec.Delete()

	err = dec.Open(inFile)
	if err != nil {
		slog.Error("Failed to open file", "error", err)
		return
	}
	defer dec.Close()

	slog.Info("Decoder state", "state", dec.GetResolvedState())

	slog.Info("Stream info",
		"current_sample", dec.TellCurrentSample(),
		"total_samples", dec.TotalSamples())

	rate, channels, bitsPerSample := dec.GetFormat()
	slog.Info("Audio format",
		"sample_rate", rate,
		"channels", channels,
		"bits_per_sample", bitsPerSample)

	fOut, err := os.Create(outFile)
	if err != nil {
		slog.Error("Failed to create output file", "error", err)
		return
	}
	defer fOut.Close()

	audioSamples := 4 * 1024
	audioBufferBytes := audioSamples * channels * 4 // 4 bytes for 16 or 24 bit samples
	audio := make([]byte, audioBufferBytes)

	for {
		sampleCnt, err := dec.DecodeSamples(audioSamples, audio)
		if err != nil {
			if err != io.EOF {
				slog.Error("Failed to decode samples", "error", err)
			}
			break
		}
		if sampleCnt == 0 {
			break
		}

		bytesToWrite := sampleCnt * channels * outBytesPerSample
		fOut.Write(audio[:bytesToWrite])
	}
	fOut.Sync()
	slog.Info("Decoding complete", "final_sample", dec.TellCurrentSample())
}
