# go-flac

Go bindings for libFLAC with a lock-free SPSC decoder.

## Features

- Lock-free implementation using atomic operations
- Thread-safe for single producer/consumer pattern
- Supports all FLAC bit depths (8, 16, 24, 32 bits)
- Supports all channel configurations (mono, stereo, 5.1, 7.1, etc.)
- Implements seek operations
- Race detector verified

## Installation

```sh
go get github.com/drgolem/go-flac
```

**Requirements:**
- Go 1.25+
- libFLAC (macOS: `brew install flac`, Ubuntu: `apt-get install libflac-dev`)

## Usage

```go
package main

import (
    "github.com/drgolem/go-flac/flac"
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

    // Process audio...
}
```

## Example

```sh
# Build and run example
go run ./examples/flac2raw input.flac output.raw

# Play with ffplay
ffplay -f s16le -ar 44100 -ch_layout stereo output.raw
```

## Testing

```sh
# Run tests
go test ./flac

# With race detector
go test -race ./flac

# Benchmarks
go test -bench=. ./flac
```

## Implementation

Uses a lock-free SPSC ring buffer for thread-safe data transfer between libFLAC callbacks and Go code. The implementation is only safe for single producer (libFLAC) and single consumer (decode loop) usage.

## License

See LICENSE file.
