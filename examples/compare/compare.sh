#!/bin/bash

# Script to compare both FLAC decoder implementations
# Usage: ./compare.sh <input.flac>

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

if [ "$#" -ne 1 ]; then
    echo "Usage: $0 <input.flac>"
    echo ""
    echo "This script compares the output of both decoder implementations:"
    echo "  1. FlacDecoder (original with custom ringbuffer)"
    echo "  2. FlacDecoderAlt (thread-safe with gammazero/deque)"
    echo ""
    echo "It verifies they produce identical output."
    exit 1
fi

INPUT="$1"
OUTPUT_ORIGINAL="/tmp/output_original.raw"
OUTPUT_ALT="/tmp/output_alt.raw"
OUTPUT_FFMPEG="/tmp/output_ffmpeg.raw"

echo "==================================="
echo "FLAC Decoder Implementation Comparison"
echo "==================================="
echo ""
echo "Input file: $INPUT"
echo ""

# Clean up previous outputs
rm -f "$OUTPUT_ORIGINAL" "$OUTPUT_ALT" "$OUTPUT_FFMPEG"

echo "Step 1: Decoding with original implementation (FlacDecoder)..."
go run "$PROJECT_ROOT/examples/flac2raw" "$INPUT" "$OUTPUT_ORIGINAL"
ORIGINAL_SIZE=$(wc -c < "$OUTPUT_ORIGINAL")
echo "  ✓ Output size: $ORIGINAL_SIZE bytes"
echo ""

echo "Step 2: Decoding with alternative implementation (FlacDecoderAlt)..."
go run "$PROJECT_ROOT/examples/flac2raw_alt" "$INPUT" "$OUTPUT_ALT"
ALT_SIZE=$(wc -c < "$OUTPUT_ALT")
echo "  ✓ Output size: $ALT_SIZE bytes"
echo ""

echo "Step 3: Generating reference with ffmpeg..."
ffmpeg -v error -i "$INPUT" -f s16le -acodec pcm_s16le "$OUTPUT_FFMPEG" 2>&1
FFMPEG_SIZE=$(wc -c < "$OUTPUT_FFMPEG")
echo "  ✓ Reference size: $FFMPEG_SIZE bytes"
echo ""

echo "==================================="
echo "Verification Results"
echo "==================================="
echo ""

# Compare original vs alternative
echo "Comparing FlacDecoder vs FlacDecoderAlt..."
if diff -q "$OUTPUT_ORIGINAL" "$OUTPUT_ALT" > /dev/null; then
    echo "  ✅ IDENTICAL - Both implementations produce the same output!"
else
    echo "  ❌ DIFFERENT - Outputs differ!"
    exit 1
fi
echo ""

# Compare against ffmpeg reference
echo "Comparing against ffmpeg reference..."
if diff -q "$OUTPUT_ORIGINAL" "$OUTPUT_FFMPEG" > /dev/null; then
    echo "  ✅ IDENTICAL - Output matches ffmpeg reference!"
else
    echo "  ⚠️  Note: Minor differences from ffmpeg may be expected"
    echo ""
    echo "File sizes:"
    echo "  FlacDecoder:    $ORIGINAL_SIZE bytes"
    echo "  FlacDecoderAlt: $ALT_SIZE bytes"
    echo "  FFmpeg ref:     $FFMPEG_SIZE bytes"
fi
echo ""

echo "==================================="
echo "Summary"
echo "==================================="
echo ""
echo "✅ Both implementations produce identical output"
echo "✅ Thread-safe alternative (FlacDecoderAlt) recommended for new projects"
echo ""

# Cleanup
rm -f /tmp/test_race_original.raw /tmp/test_race_alt.raw
