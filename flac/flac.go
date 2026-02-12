package flac

/*
#cgo pkg-config: flac
#include <FLAC/format.h>
#include <FLAC/stream_decoder.h>
#include <stdlib.h>

extern int
get_decoder_channels(FLAC__StreamMetadata *metadata);

extern int
get_decoder_depth(FLAC__StreamMetadata *metadata);

extern int
get_decoder_rate(FLAC__StreamMetadata *metadata);

extern FLAC__uint64
get_total_samples(FLAC__StreamMetadata *metadata);

extern void
decoderErrorCallback_cgo(const FLAC__StreamDecoder *,
                 FLAC__StreamDecoderErrorStatus,
                 void *);

extern void
decoderMetadataCallback_cgo(const FLAC__StreamDecoder *,
                const FLAC__StreamMetadata *,
                void *);

extern FLAC__StreamDecoderWriteStatus
decoderWriteCallback_cgo(const FLAC__StreamDecoder *,
                 const FLAC__Frame *,
                 const FLAC__int32 **,
                 void *);
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/cgo"
	"unsafe"

	"github.com/drgolem/ringbuffer"
)

//type AudioFrameDecoder interface {
//	Open(filePath string) error
//	Close() error
//
//	TotalSamples() int64
//	TellCurrentSample() int64
//	GetFormat() (rate int, channels int, bitsPerSample int)
//
//	// Decodes audio samples, returns number of samples
//	DecodeSamples(samples int, audio []byte) (int, error)
//
//	io.Seeker
//}

func GetVersion() string {
	return C.GoString(C.FLAC__VERSION_STRING)
}

// FlacDecoder provides thread-safe FLAC audio decoding using a lock-free
// SPSC (Single Producer Single Consumer) ring buffer.
//
// THREAD SAFETY: FlacDecoder is THREAD-SAFE for the specific pattern where:
//   - libFLAC callbacks write to the buffer (single producer - C thread)
//   - Go code reads from the buffer (single consumer - Go goroutine)
//
// This is achieved using a lock-free ring buffer with atomic operations, providing
// better performance than mutex-based synchronization while maintaining safety.
//
// IMPORTANT: The decoder itself should still be used from a single goroutine for
// methods like DecodeSamples(), Seek(), Open(), and Close(). The thread-safety
// applies to the internal buffer operations between CGO callbacks and Go code.
type FlacDecoder struct {
	decoder  *C.FLAC__StreamDecoder
	hDecoder cgo.Handle

	rate                    int64
	channels                int
	bitsPerSample           int
	outputBytesPerSample    int
	currentSample           int64
	totalSamples            int64
	maxOutputSampleBitDepth int
	streamBytesPerSample    int

	// Lock-free SPSC ring buffer for thread-safe audio data transfer
	ringBuffer *ringbuffer.RingBuffer
	b16        [2]byte
	b24        [3]byte

	// Error state from decoder callbacks
	lastError error
}

const (
	ringBufferCapacity = 2 * 2 * 4 * 4096

	// Valid bit depths for FLAC audio
	bitDepth8  = 8
	bitDepth16 = 16
	bitDepth24 = 24
	bitDepth32 = 32
)

// NewFlacFrameDecoder creates a new thread-safe FLAC decoder.
//
// Parameters:
//   - maxOutputSampleBitDepth: Output bit depth (8, 16, 24, or 32)
//
// Returns the decoder instance or an error if the bit depth is unsupported.
//
// The decoder uses a lock-free ring buffer for thread-safe data transfer between
// libFLAC's C callbacks and Go code, providing better performance than mutex-based
// approaches while maintaining safety.
func NewFlacFrameDecoder(maxOutputSampleBitDepth int) (*FlacDecoder, error) {
	// Validate bit depth
	if maxOutputSampleBitDepth != bitDepth8 && maxOutputSampleBitDepth != bitDepth16 &&
		maxOutputSampleBitDepth != bitDepth24 && maxOutputSampleBitDepth != bitDepth32 {
		return nil, fmt.Errorf("invalid maxOutputSampleBitDepth: %d, must be 8, 16, 24, or 32", maxOutputSampleBitDepth)
	}

	dec := &FlacDecoder{
		maxOutputSampleBitDepth: maxOutputSampleBitDepth,
		outputBytesPerSample:    maxOutputSampleBitDepth / 8,
		ringBuffer:              ringbuffer.New(ringBufferCapacity),
	}

	dec.decoder = C.FLAC__stream_decoder_new()
	if dec.decoder == nil {
		return nil, errors.New("failed to create FLAC decoder")
	}

	// Store handle for callbacks
	dec.hDecoder = cgo.NewHandle(dec)

	return dec, nil
}

// Delete cleans up the decoder and frees resources.
// Must be called when done with the decoder to prevent memory leaks.
func (d *FlacDecoder) Delete() error {
	if d.decoder != nil {
		C.FLAC__stream_decoder_delete(d.decoder)
		d.decoder = nil
	}
	d.hDecoder.Delete()
	return nil
}

// GetResolvedState returns the current decoder state as a string.
func (d *FlacDecoder) GetResolvedState() string {
	if d.decoder == nil {
		return "DECODER_NOT_INITIALIZED"
	}
	return C.GoString(C.FLAC__stream_decoder_get_resolved_state_string(d.decoder))
}

// Open opens a FLAC file for decoding.
func (d *FlacDecoder) Open(filePath string) error {
	if d.decoder == nil {
		return errors.New("decoder not initialized")
	}

	filename := C.CString(filePath)
	defer C.free(unsafe.Pointer(filename))

	writeCallback := C.FLAC__StreamDecoderWriteCallback(unsafe.Pointer(C.decoderWriteCallback_cgo))
	metadataCallback := C.FLAC__StreamDecoderMetadataCallback(unsafe.Pointer(C.decoderMetadataCallback_cgo))
	errorCallback := C.FLAC__StreamDecoderErrorCallback(unsafe.Pointer(C.decoderErrorCallback_cgo))

	// Reset decoder state
	d.rate = 0
	d.channels = 0
	d.bitsPerSample = 0
	d.outputBytesPerSample = d.maxOutputSampleBitDepth / 8
	d.currentSample = 0
	d.totalSamples = 0
	d.lastError = nil
	d.ringBuffer.Reset()

	status := C.FLAC__stream_decoder_init_file(d.decoder, filename,
		writeCallback,
		metadataCallback,
		errorCallback,
		unsafe.Pointer(&d.hDecoder),
	)

	if status != C.FLAC__STREAM_DECODER_INIT_STATUS_OK {
		errStr := getStreamDecoderInitStatusString(status)
		return fmt.Errorf("init flac error: %s", errStr)
	}

	if C.FLAC__stream_decoder_process_until_end_of_metadata(d.decoder) == 0 {
		state := C.FLAC__stream_decoder_get_state(d.decoder)
		return fmt.Errorf("decode metadata error: %d", state)
	}

	return nil
}

// Close closes the decoder.
func (d *FlacDecoder) Close() error {
	if d.decoder != nil {
		C.FLAC__stream_decoder_finish(d.decoder)
	}

	// Reset decoder state
	d.rate = 0
	d.channels = 0
	d.bitsPerSample = 0
	d.outputBytesPerSample = 0
	d.currentSample = 0
	d.totalSamples = 0
	d.lastError = nil
	d.ringBuffer.Reset()

	return nil
}

// TotalSamples returns the total number of samples in the stream.
func (d *FlacDecoder) TotalSamples() int64 {
	return d.totalSamples
}

// TellCurrentSample returns the current sample position.
func (d *FlacDecoder) TellCurrentSample() int64 {
	return d.currentSample
}

// GetFormat returns the audio format parameters.
func (d *FlacDecoder) GetFormat() (int, int, int) {
	return int(d.rate), d.channels, d.bitsPerSample
}

// DecodeSamples decodes the specified number of audio samples into the provided buffer.
//
// Parameters:
//   - samples: Number of samples to decode (per channel)
//   - audio: Output buffer for decoded audio data
//
// Returns the number of samples decoded and any error encountered.
//
// The buffer must be large enough: samples * channels * bytesPerSample
func (d *FlacDecoder) DecodeSamples(samples int, audio []byte) (int, error) {
	// Validation checks
	if samples <= 0 {
		return 0, errors.New("samples must be positive")
	}

	if d.channels <= 0 || d.outputBytesPerSample <= 0 {
		return 0, errors.New("decoder not initialized: channels or outputBytesPerSample invalid")
	}

	// Check for potential integer overflow in byte calculation
	const maxInt = int(^uint(0) >> 1)
	if samples > maxInt/(d.channels*d.outputBytesPerSample) {
		return 0, errors.New("samples value too large, would cause integer overflow")
	}

	bytesRequired := samples * d.channels * d.outputBytesPerSample
	if len(audio) < bytesRequired {
		return 0, fmt.Errorf("audio buffer too small: need %d bytes, got %d", bytesRequired, len(audio))
	}

	// Reset error state
	d.lastError = nil

	samplesToRead := samples
	samplesRead := 0

	for samplesToRead > 0 {
		// Check for errors from callbacks
		if d.lastError != nil {
			err := d.lastError
			d.lastError = nil // Clear the error after reporting
			return samplesRead, err
		}

		// Check decoder state first
		state := C.FLAC__stream_decoder_get_state(d.decoder)

		// Check available data in buffer
		available := d.ringBuffer.AvailableRead()
		availableSamples := int(available) / (d.channels * d.outputBytesPerSample)

		// If we're at end of stream or have enough data, read from buffer
		if state == C.FLAC__STREAM_DECODER_END_OF_STREAM || availableSamples >= samplesToRead {
			offset := samplesRead * d.channels * d.outputBytesPerSample

			// Determine how many bytes to read
			bytesToRead := samplesToRead * d.channels * d.outputBytesPerSample
			if state == C.FLAC__STREAM_DECODER_END_OF_STREAM {
				// At EOF, read whatever is available
				bytesToRead = int(available)
			}

			if bytesToRead > 0 {
				n, err := d.ringBuffer.Read(audio[offset : offset+bytesToRead])
				if err != nil {
					return samplesRead, fmt.Errorf("failed to read from buffer: %w", err)
				}
				readSamples := n / (d.channels * d.outputBytesPerSample)
				samplesRead += readSamples
				samplesToRead -= readSamples
				d.currentSample += int64(readSamples)
			}

			// If at EOF and no more data to read, we're done
			if state == C.FLAC__STREAM_DECODER_END_OF_STREAM {
				if samplesRead > 0 {
					return samplesRead, nil
				}
				return 0, io.EOF
			}

			// If we have enough data, we're done
			if samplesToRead <= 0 {
				break
			}
		}

		// Need more data, decode more frames
		res := C.FLAC__stream_decoder_process_single(d.decoder)

		if res == 0 {
			// Check for errors from callbacks
			if d.lastError != nil {
				err := d.lastError
				d.lastError = nil
				return samplesRead, err
			}

			state := C.FLAC__stream_decoder_get_state(d.decoder)
			if state == C.FLAC__STREAM_DECODER_END_OF_STREAM {
				// Try to read any remaining data in buffer
				available := d.ringBuffer.AvailableRead()
				if available > 0 {
					continue // Loop will handle reading remaining data
				}
				if samplesRead > 0 {
					return samplesRead, nil
				}
				return 0, io.EOF
			}

			return samplesRead, fmt.Errorf("decode samples error: %d", state)
		}

		// Check for callback errors
		if d.lastError != nil {
			err := d.lastError
			d.lastError = nil
			return samplesRead, err
		}
	}

	return samplesRead, nil
}

// Seek seeks to the specified sample position.
func (d *FlacDecoder) Seek(offset int64, whence int) (int64, error) {
	seekSample := offset
	if whence == io.SeekCurrent {
		seekSample = d.currentSample + offset
	} else if whence == io.SeekEnd {
		seekSample = d.totalSamples + offset
	}

	// Validate seek position
	if seekSample < 0 {
		return d.currentSample, errors.New("cannot seek before start of stream")
	}
	if d.totalSamples > 0 && seekSample >= d.totalSamples {
		return d.currentSample, fmt.Errorf("cannot seek beyond end of stream (pos: %d, total: %d)", seekSample, d.totalSamples)
	}

	// Reset ring buffer to discard stale data
	d.ringBuffer.Reset()

	res := C.FLAC__stream_decoder_seek_absolute(d.decoder, C.FLAC__uint64(seekSample))
	if res == 0 {
		state := C.FLAC__stream_decoder_get_state(d.decoder)
		return d.currentSample, fmt.Errorf("seek failed, decoder state: %d", state)
	}

	d.currentSample = seekSample

	return d.currentSample, nil
}

// setError stores an error from decoder callbacks
func (d *FlacDecoder) setError(err error) {
	if d.lastError == nil {
		d.lastError = err
	}
}

//export decoderErrorCallback
func decoderErrorCallback(d *C.FLAC__StreamDecoder, status C.FLAC__StreamDecoderErrorStatus, data unsafe.Pointer) {
	h := *(*cgo.Handle)(data)
	dec := h.Value().(*FlacDecoder)

	var errMsg string
	switch int(status) {
	case 0: // FLAC__STREAM_DECODER_ERROR_STATUS_LOST_SYNC
		errMsg = "lost sync"
	case 1: // FLAC__STREAM_DECODER_ERROR_STATUS_BAD_HEADER
		errMsg = "bad header"
	case 2: // FLAC__STREAM_DECODER_ERROR_STATUS_FRAME_CRC_MISMATCH
		errMsg = "frame CRC mismatch"
	case 3: // FLAC__STREAM_DECODER_ERROR_STATUS_UNPARSEABLE_STREAM
		errMsg = "unparseable stream"
	case 4: // FLAC__STREAM_DECODER_ERROR_STATUS_BAD_METADATA
		errMsg = "bad metadata"
	case 5: // FLAC__STREAM_DECODER_ERROR_STATUS_OUT_OF_BOUNDS
		errMsg = "out of bounds"
	case 6: // FLAC__STREAM_DECODER_ERROR_STATUS_MISSING_FRAME
		errMsg = "missing frame"
	default:
		errMsg = fmt.Sprintf("unknown error status: %d", status)
	}

	dec.lastError = fmt.Errorf("FLAC decoder error: %s", errMsg)
	slog.Error("FLAC decoder error callback", "error", errMsg, "status", int(status))
}

//export decoderWriteCallback
func decoderWriteCallback(decoder *C.FLAC__StreamDecoder, frame *C.FLAC__Frame, buffer **C.FLAC__int32, client_data unsafe.Pointer) C.FLAC__StreamDecoderWriteStatus {

	h := *(*cgo.Handle)(client_data)
	dec := h.Value().(*FlacDecoder)

	sampleCount := int64(frame.header.blocksize)
	if sampleCount == 0 {
		return C.FLAC__STREAM_DECODER_WRITE_STATUS_CONTINUE
	}

	// Validate channels have been initialized
	if dec.channels == 0 {
		err := errors.New("channels not initialized - metadata callback not called")
		slog.Error("Write callback error", "error", err)
		dec.setError(err)
		return C.FLAC__STREAM_DECODER_WRITE_STATUS_ABORT
	}

	// Use the actual number of channels from the decoder metadata
	numChannels := dec.channels
	chSlice := unsafe.Slice(buffer, numChannels)

	// Create slices for each channel
	channels := make([][]C.FLAC__int32, numChannels)
	for ch := 0; ch < numChannels; ch++ {
		channels[ch] = unsafe.Slice(chSlice[ch], sampleCount)
	}

	// Interleave samples from all channels
	for i := int64(0); i < sampleCount; i++ {
		for ch := 0; ch < dec.channels; ch++ {
			sample := int32(channels[ch][i])

			switch dec.streamBytesPerSample {
			case 3:
				int32toInt24LEBytes(sample, &dec.b24)
				if dec.maxOutputSampleBitDepth == bitDepth24 {
					if _, err := dec.ringBuffer.Write(dec.b24[:3]); err != nil {
						slog.Error("Failed to write 24-bit sample", "error", err)
						dec.setError(err)
						return C.FLAC__STREAM_DECODER_WRITE_STATUS_ABORT
					}
				} else {
					if _, err := dec.ringBuffer.Write(dec.b24[1:3]); err != nil {
						slog.Error("Failed to write 16-bit sample from 24-bit", "error", err)
						dec.setError(err)
						return C.FLAC__STREAM_DECODER_WRITE_STATUS_ABORT
					}
				}
			case 2:
				dec.b16[0] = byte(sample)
				dec.b16[1] = byte(sample >> 8)
				if _, err := dec.ringBuffer.Write(dec.b16[:2]); err != nil {
					slog.Error("Failed to write 16-bit sample", "error", err)
					dec.setError(err)
					return C.FLAC__STREAM_DECODER_WRITE_STATUS_ABORT
				}
			case 1:
				// 8-bit samples
				if _, err := dec.ringBuffer.Write([]byte{byte(sample)}); err != nil {
					slog.Error("Failed to write 8-bit sample", "error", err)
					dec.setError(err)
					return C.FLAC__STREAM_DECODER_WRITE_STATUS_ABORT
				}
			case 4:
				// 32-bit samples (little-endian)
				if _, err := dec.ringBuffer.Write([]byte{
					byte(sample),
					byte(sample >> 8),
					byte(sample >> 16),
					byte(sample >> 24),
				}); err != nil {
					slog.Error("Failed to write 32-bit sample", "error", err)
					dec.setError(err)
					return C.FLAC__STREAM_DECODER_WRITE_STATUS_ABORT
				}
			default:
				// Should never happen if validation is correct
				slog.Error("unsupported stream bytes per sample", "bytes", dec.streamBytesPerSample)
				err := fmt.Errorf("unsupported stream bytes per sample: %d", dec.streamBytesPerSample)
				dec.setError(err)
				return C.FLAC__STREAM_DECODER_WRITE_STATUS_ABORT
			}
		}
	}

	return C.FLAC__STREAM_DECODER_WRITE_STATUS_CONTINUE
}

//export decoderMetadataCallback
func decoderMetadataCallback(d *C.FLAC__StreamDecoder, metadata *C.FLAC__StreamMetadata, client_data unsafe.Pointer) {
	h := *(*cgo.Handle)(client_data)
	dec := h.Value().(*FlacDecoder)

	if metadata._type == C.FLAC__METADATA_TYPE_STREAMINFO {
		dec.channels = int(C.get_decoder_channels(metadata))
		dec.bitsPerSample = int(C.get_decoder_depth(metadata))
		dec.rate = int64(C.get_decoder_rate(metadata))
		dec.streamBytesPerSample = (dec.bitsPerSample + 7) / 8
		dec.totalSamples = int64(C.get_total_samples(metadata))
	}
}

func getStreamDecoderInitStatusString(status C.FLAC__StreamDecoderInitStatus) string {
	var theCArray **C.char = (**C.char)(unsafe.Pointer(&C.FLAC__StreamDecoderInitStatusString))
	length := 5
	slice := unsafe.Slice(theCArray, length)

	return C.GoString(slice[status])
}

func int32toInt24LEBytes(n int32, out *[3]byte) {
	if (n & 0x800000) > 0 {
		n |= ^0xffffff
	}
	out[2] = byte(n >> 16)
	out[1] = byte(n >> 8)
	out[0] = byte(n)
}
