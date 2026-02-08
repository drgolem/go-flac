# Examples

This directory contains example programs demonstrating the FLAC decoder.

## Directory Structure

```
examples/
├── flac2raw/     - FLAC to raw PCM converter (thread-safe lock-free decoder)
│   ├── main.go
│   └── go.mod
└── compare/      - Comparison and testing tools
    └── compare.sh
```

## Building the Example

```bash
cd examples/flac2raw
go build
./flac2raw input.flac output.raw
```

## Running the Example

### Quick Run (without building)
```bash
go run ./examples/flac2raw input.flac output.raw
```

### Play Output
```bash
ffplay -ar 44100 -ch_layout stereo -f s16le output.raw
```

## Implementation Details

The flac2raw example uses **FlacDecoder**, a thread-safe lock-free SPSC (Single Producer Single Consumer) decoder that provides:

- ✅ **Lock-free SPSC thread-safety** with zero overhead
- ✅ **~100% performance** (atomic operations, no mutexes)
- ✅ **Race-free** (passes race detector)
- ✅ **Production-ready** for all projects

See [../PERFORMANCE_ANALYSIS.md](../PERFORMANCE_ANALYSIS.md) for detailed benchmarks.

## Example Output

```
$ go run ./examples/flac2raw input.flac output.raw

INFO FLAC to RAW converter
INFO Processing files input=input.flac output=output.raw
INFO libFLAC version version=1.5.0
INFO Decoder state state=FLAC__STREAM_DECODER_SEARCH_FOR_FRAME_SYNC
INFO Stream info current_sample=0 total_samples=264600
INFO Audio format sample_rate=44100 channels=2 bits_per_sample=16
INFO Decoding complete final_sample=264600
```

## Testing with Different Files

The decoder supports various FLAC formats:
- ✅ 8, 16, 24, 32-bit samples
- ✅ Mono, stereo, 5.1, 7.1 channels
- ✅ Any sample rate

## Performance Testing

See [../PERFORMANCE_ANALYSIS.md](../PERFORMANCE_ANALYSIS.md) for detailed benchmarks.

Quick benchmark:
```bash
time go run ./examples/flac2raw input.flac output.raw
```

Expected results:
- **Thread-safe** with ~0% overhead
- **High throughput**: > 100 Msamples/sec

## Development

The example is a separate Go module with its own `go.mod` using `replace` directives to reference the main library. This allows:
- Independent compilation
- Separate testing
- No package conflicts
- Clean project structure
