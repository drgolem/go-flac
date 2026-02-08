# go-flac

Go bindings for libFLAC with a high-performance, thread-safe decoder using lock-free SPSC (Single Producer Single Consumer) architecture.

## Features

✅ **Thread-safe** with lock-free ring buffer (zero overhead)
✅ Supports all FLAC bit depths (8, 16, 24, 32 bits)
✅ Supports all channel configurations (mono, stereo, 5.1, 7.1, etc.)
✅ Proper error handling and validation
✅ Seek support
✅ Comprehensive test coverage
✅ Race detector verified

## Quick Start

```sh
go run ./examples/flac2raw input.flac output.raw
```

### Play the Output

```sh
ffplay -ar 44100 -ch_layout stereo -f s16le output.raw
```

## API Usage

```go
package main

import (
    "github.com/drgolem/go-flac/pkg/flac"
)

func main() {
    // Create decoder with 16-bit output
    dec, err := flac.NewFlacFrameDecoder(16)
    if err != nil {
        panic(err)
    }
    defer dec.Delete()

    // Open FLAC file
    err = dec.Open("input.flac")
    if err != nil {
        panic(err)
    }
    defer dec.Close()

    // Get audio format
    rate, channels, bitsPerSample := dec.GetFormat()

    // Decode samples
    audio := make([]byte, 4096*channels*2)
    samples, err := dec.DecodeSamples(4096, audio)

    // ... process audio
}
```

## Implementation Details

**FlacDecoder** uses a lock-free SPSC ring buffer for thread-safe audio data transfer between libFLAC's C callbacks and Go code. This design provides:

- **Zero synchronization overhead** - Atomic operations instead of mutexes
- **Thread-safety** - Safe for single producer (libFLAC) + single consumer (your code)
- **High performance** - ~100% baseline performance with added safety
- **Real-time friendly** - Predictable latency, no lock contention

See [LOCKFREE_IMPLEMENTATION.md](LOCKFREE_IMPLEMENTATION.md) for detailed architecture.

## Testing & Validation

### Run Tests

```sh
# All tests
go test ./pkg/flac

# With race detector
go test -race ./pkg/flac

# Benchmarks
go test -bench=. -benchmem ./pkg/flac
```

### Manual Verification

```sh
# Generate reference with ffmpeg
ffmpeg -i input.flac -f s16le -acodec pcm_s16le reference.raw

# Decode with go-flac
go run ./examples/flac2raw input.flac output.raw

# Compare outputs
diff reference.raw output.raw
```

### Race Detection

```sh
# Test for data races
go test -race ./pkg/flac

# Run example with race detector
go run -race ./examples/flac2raw input.flac output.raw
```

## Performance

The decoder achieves excellent performance with zero overhead compared to non-thread-safe approaches:

- **Throughput**: > 100 Msamples/sec on modern hardware
- **CPU Usage**: < 1% for 44.1kHz stereo decoding
- **Memory**: Fixed allocation, zero allocations during decode loop
- **Overhead**: ~0% compared to non-thread-safe baseline

See [PERFORMANCE_ANALYSIS.md](PERFORMANCE_ANALYSIS.md) for detailed benchmarks.

## Requirements

- Go 1.25+
- libFLAC (install via package manager)
  - **macOS**: `brew install flac`
  - **Ubuntu**: `apt-get install libflac-dev`
  - **Arch**: `pacman -S flac`

## Status

Production-ready. The decoder uses lock-free atomic operations for thread-safety with zero performance overhead. Suitable for:

- Real-time audio processing
- High-performance applications
- Multi-threaded environments
- Low-latency systems

## License

See LICENSE file for details.
