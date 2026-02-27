# go-flac

Go bindings for libFLAC — decoder and encoder.

## Features

### Decoder
- Lock-free SPSC ring buffer for thread-safe callback-to-Go data transfer
- Supports all FLAC bit depths (8, 16, 24, 32 bits)
- Supports all channel configurations (mono, stereo, 5.1, 7.1, etc.)
- Seek support
- Race detector verified

### Encoder
- File mode: encode directly to `.flac` file
- Stream mode: collect encoded bytes in memory (for network streaming)
- Configurable compression level (0–8)
- STREAMINFO metadata extraction
- `PCMToInt32` utility for converting raw PCM bytes to encoder input
- Supports 8, 16, 24, and 32-bit encoding

## Installation

```sh
go get github.com/drgolem/go-flac
```

**Requirements:**
- Go 1.25+
- libFLAC (macOS: `brew install flac`, Ubuntu: `apt-get install libflac-dev`)

## Usage

### Decoding

```go
dec, err := flac.NewFlacFrameDecoder(16)
if err != nil {
    return err
}
defer dec.Delete()

if err := dec.Open("input.flac"); err != nil {
    return err
}
defer dec.Close()

rate, channels, bps := dec.GetFormat()
buf := make([]byte, 4096*channels*(bps/8))

for {
    n, err := dec.DecodeSamples(4096, buf)
    if err == io.EOF || n == 0 {
        break
    }
    // process buf[:n*channels*(bps/8)]
}
```

### Encoding to file

```go
enc, err := flac.NewFlacEncoder(44100, 2, 16)
if err != nil {
    return err
}
defer enc.Close()

if err := enc.InitFile("output.flac"); err != nil {
    return err
}

// samples is []int32 with interleaved channels
if err := enc.ProcessInterleaved(samples, numSamples); err != nil {
    return err
}

if err := enc.Finish(); err != nil {
    return err
}
```

### Encoding to stream (in-memory)

```go
enc, err := flac.NewFlacEncoder(48000, 2, 16)
if err != nil {
    return err
}
defer enc.Close()

if err := enc.InitStream(); err != nil {
    return err
}

if err := enc.ProcessInterleaved(samples, numSamples); err != nil {
    return err
}

// Retrieve encoded bytes
encoded := enc.TakeBytes()

if err := enc.Finish(); err != nil {
    return err
}
// TakeBytes() again for any remaining data after Finish
remaining := enc.TakeBytes()
```

### PCM to int32 conversion

```go
// Convert raw little-endian PCM bytes to int32 for the encoder
pcmData := readRawPCM()             // []byte
out := make([]int32, len(pcmData)/2) // for 16-bit
flac.PCMToInt32(pcmData, 16, out)

enc.ProcessInterleaved(out, len(out)/channels)
```

## Examples

```sh
# Decode FLAC to raw PCM
go run ./examples/flac2raw input.flac output.raw

# Play decoded PCM with ffplay
ffplay -f s16le -ar 44100 -ch_layout stereo output.raw

# Encode raw PCM to FLAC
go run ./examples/raw2flac input.raw output.flac 44100 2 16

# Verify with ffprobe
ffprobe output.flac
```

## Testing

```sh
# Run all tests (decoder + encoder + roundtrip)
go test -v ./flac

# With race detector
go test -race ./flac

# End-to-end roundtrip with a real FLAC file
FLAC_TEST_FILE=path/to/test.flac go test -v ./flac -run TestRoundtrip_FlacFile
```

### Test coverage

- **Encoder**: parameter validation, compression levels, file/stream modes, mono/stereo, 16/24-bit, error conditions
- **PCMToInt32**: 8, 16, 24, 32-bit conversion with edge values
- **Roundtrip (synthetic)**: encode → decode → compare for 4 format combinations (8/16/24-bit, mono/stereo)
- **Roundtrip (file)**: decode real FLAC → re-encode → decode → byte-for-byte PCM comparison

## License

See LICENSE file.
