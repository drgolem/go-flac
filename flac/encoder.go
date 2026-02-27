package flac

/*
#cgo pkg-config: flac
#include <FLAC/stream_encoder.h>
#include <FLAC/metadata.h>
#include <stdlib.h>
#include <string.h>

extern unsigned int get_min_blocksize(FLAC__StreamMetadata *metadata);
extern unsigned int get_max_blocksize(FLAC__StreamMetadata *metadata);
extern unsigned int get_min_framesize(FLAC__StreamMetadata *metadata);
extern unsigned int get_max_framesize(FLAC__StreamMetadata *metadata);
extern int get_decoder_channels(FLAC__StreamMetadata *metadata);
extern int get_decoder_depth(FLAC__StreamMetadata *metadata);
extern int get_decoder_rate(FLAC__StreamMetadata *metadata);
extern FLAC__uint64 get_total_samples(FLAC__StreamMetadata *metadata);
extern void get_md5_signature(FLAC__StreamMetadata *metadata, uint8_t *out);

extern FLAC__StreamEncoderWriteStatus
encoderWriteCallback_cgo(const FLAC__StreamEncoder *encoder,
                         const FLAC__byte buffer[],
                         size_t bytes,
                         uint32_t samples,
                         uint32_t current_frame,
                         void *client_data);

extern void
encoderMetadataCallback_cgo(const FLAC__StreamEncoder *encoder,
                            const FLAC__StreamMetadata *metadata,
                            void *client_data);
*/
import "C"

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime/cgo"
	"sync"
	"unsafe"
)

// FlacEncoder provides FLAC encoding using libFLAC's stream encoder.
//
// It can encode either to a file (via EncodeFile) or to a callback-based
// stream (via InitStream + ProcessInterleaved + Finish). The stream mode
// is ideal for Ogg FLAC wrapping where each encoded frame is delivered
// to a write callback.
//
// THREAD SAFETY: FlacEncoder is NOT thread-safe. All methods must be called
// from a single goroutine. The internal write callback uses a mutex to
// safely transfer data from the C callback to the Go side.
type FlacEncoder struct {
	encoder  *C.FLAC__StreamEncoder
	hEncoder cgo.Handle

	sampleRate       int
	channels         int
	bitsPerSample    int
	compressionLevel int

	// Stream mode: write callback collects encoded bytes here
	mu        sync.Mutex
	outBuf    []byte // accumulated output from write callbacks
	lastError error

	// Metadata captured from metadata callback (after Finish)
	streamInfo []byte // raw STREAMINFO block (34 bytes)

	initialized bool
}

// NewFlacEncoder creates a new FLAC encoder.
//
// Parameters:
//   - sampleRate: Sample rate in Hz (e.g., 44100, 48000, 96000)
//   - channels: Number of audio channels (1-8)
//   - bitsPerSample: Bit depth (8, 16, 24, or 32)
//
// Returns the encoder instance or an error if parameters are invalid.
func NewFlacEncoder(sampleRate, channels, bitsPerSample int) (*FlacEncoder, error) {
	if sampleRate < 1 || sampleRate > 655350 {
		return nil, fmt.Errorf("invalid sample rate: %d (must be 1-655350)", sampleRate)
	}
	if channels < 1 || channels > 8 {
		return nil, fmt.Errorf("invalid channels: %d (must be 1-8)", channels)
	}
	if bitsPerSample != bitDepth8 && bitsPerSample != bitDepth16 &&
		bitsPerSample != bitDepth24 && bitsPerSample != bitDepth32 {
		return nil, fmt.Errorf("invalid bitsPerSample: %d (must be 8, 16, 24, or 32)", bitsPerSample)
	}

	enc := C.FLAC__stream_encoder_new()
	if enc == nil {
		return nil, errors.New("failed to create FLAC encoder")
	}

	e := &FlacEncoder{
		encoder:          enc,
		sampleRate:       sampleRate,
		channels:         channels,
		bitsPerSample:    bitsPerSample,
		compressionLevel: 5, // libFLAC default
	}

	e.hEncoder = cgo.NewHandle(e)

	return e, nil
}

// SetCompressionLevel sets the compression level (0=fastest, 8=best).
// Must be called before Init* methods. Default is 5.
func (e *FlacEncoder) SetCompressionLevel(level int) error {
	if level < 0 || level > 8 {
		return fmt.Errorf("invalid compression level: %d (must be 0-8)", level)
	}
	e.compressionLevel = level
	return nil
}

// configureEncoder sets the encoder parameters. Called before init.
func (e *FlacEncoder) configureEncoder() error {
	if C.FLAC__stream_encoder_set_channels(e.encoder, C.uint32_t(e.channels)) == 0 {
		return errors.New("failed to set channels")
	}
	if C.FLAC__stream_encoder_set_bits_per_sample(e.encoder, C.uint32_t(e.bitsPerSample)) == 0 {
		return errors.New("failed to set bits per sample")
	}
	if C.FLAC__stream_encoder_set_sample_rate(e.encoder, C.uint32_t(e.sampleRate)) == 0 {
		return errors.New("failed to set sample rate")
	}
	if C.FLAC__stream_encoder_set_compression_level(e.encoder, C.uint32_t(e.compressionLevel)) == 0 {
		return errors.New("failed to set compression level")
	}
	if C.FLAC__stream_encoder_set_verify(e.encoder, C.FLAC__bool(1)) == 0 {
		return errors.New("failed to enable verify")
	}
	return nil
}

// InitFile initializes the encoder to write to a file.
// Call ProcessInterleaved to feed audio data, then Finish to finalize.
func (e *FlacEncoder) InitFile(filePath string) error {
	if e.encoder == nil {
		return errors.New("encoder not initialized")
	}
	if e.initialized {
		return errors.New("encoder already initialized")
	}

	if err := e.configureEncoder(); err != nil {
		return err
	}

	filename := C.CString(filePath)
	defer C.free(unsafe.Pointer(filename))

	status := C.FLAC__stream_encoder_init_file(e.encoder, filename, nil, nil)
	if status != C.FLAC__STREAM_ENCODER_INIT_STATUS_OK {
		return fmt.Errorf("init encoder error: %s", getStreamEncoderInitStatusString(status))
	}

	e.initialized = true
	return nil
}

// InitStream initializes the encoder in stream mode with write callback.
// Encoded data is collected internally and returned via TakeBytes().
// This mode is ideal for Ogg FLAC wrapping or piping to a network sink.
func (e *FlacEncoder) InitStream() error {
	if e.encoder == nil {
		return errors.New("encoder not initialized")
	}
	if e.initialized {
		return errors.New("encoder already initialized")
	}

	if err := e.configureEncoder(); err != nil {
		return err
	}

	writeCallback := C.FLAC__StreamEncoderWriteCallback(unsafe.Pointer(C.encoderWriteCallback_cgo))
	metadataCallback := C.FLAC__StreamEncoderMetadataCallback(unsafe.Pointer(C.encoderMetadataCallback_cgo))

	status := C.FLAC__stream_encoder_init_stream(
		e.encoder,
		writeCallback,
		nil, // no seek callback
		nil, // no tell callback
		metadataCallback,
		unsafe.Pointer(&e.hEncoder),
	)
	if status != C.FLAC__STREAM_ENCODER_INIT_STATUS_OK {
		return fmt.Errorf("init stream encoder error: %s", getStreamEncoderInitStatusString(status))
	}

	e.initialized = true
	return nil
}

// SetTotalSamplesEstimate provides a hint to the encoder about the total
// number of samples. This improves STREAMINFO accuracy but is not required.
// Must be called after NewFlacEncoder but before Init*.
func (e *FlacEncoder) SetTotalSamplesEstimate(totalSamples int64) error {
	if e.initialized {
		return errors.New("cannot set total samples after initialization")
	}
	if C.FLAC__stream_encoder_set_total_samples_estimate(e.encoder, C.FLAC__uint64(totalSamples)) == 0 {
		return errors.New("failed to set total samples estimate")
	}
	return nil
}

// ProcessInterleaved feeds interleaved int32 PCM samples to the encoder.
//
// Each sample should be a signed int32 right-justified to bitsPerSample.
// For 16-bit audio, samples should be in [-32768, 32767].
// For 24-bit audio, samples should be in [-8388608, 8388607].
//
// The samples slice must contain numSamples * channels values.
func (e *FlacEncoder) ProcessInterleaved(samples []int32, numSamples int) error {
	if !e.initialized {
		return errors.New("encoder not initialized")
	}
	if numSamples <= 0 {
		return errors.New("numSamples must be positive")
	}
	if len(samples) < numSamples*e.channels {
		return fmt.Errorf("samples slice too small: need %d, got %d", numSamples*e.channels, len(samples))
	}

	ok := C.FLAC__stream_encoder_process_interleaved(
		e.encoder,
		(*C.FLAC__int32)(unsafe.Pointer(&samples[0])),
		C.uint32_t(numSamples),
	)
	if ok == 0 {
		state := C.FLAC__stream_encoder_get_state(e.encoder)
		return fmt.Errorf("process interleaved failed, encoder state: %d", state)
	}

	return nil
}

// TakeBytes returns any encoded bytes accumulated from write callbacks
// and clears the internal buffer. Only valid in stream mode (InitStream).
func (e *FlacEncoder) TakeBytes() []byte {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.outBuf) == 0 {
		return nil
	}
	out := e.outBuf
	e.outBuf = nil
	return out
}

// StreamInfo returns the raw STREAMINFO metadata (34 bytes) captured
// after Finish(). Returns nil if Finish hasn't been called yet or
// if the encoder was not in stream mode.
func (e *FlacEncoder) StreamInfo() []byte {
	return e.streamInfo
}

// Finish finalizes the encoding, flushing any remaining data.
// After Finish, the encoder can be deleted or re-initialized.
func (e *FlacEncoder) Finish() error {
	if e.encoder == nil {
		return errors.New("encoder not initialized")
	}

	ok := C.FLAC__stream_encoder_finish(e.encoder)
	e.initialized = false

	if ok == 0 {
		return errors.New("encoder finish failed (possible verify mismatch)")
	}
	return nil
}

// Close releases all C resources. Must be called when done with the encoder.
func (e *FlacEncoder) Close() {
	if e.encoder != nil {
		if e.initialized {
			C.FLAC__stream_encoder_finish(e.encoder)
			e.initialized = false
		}
		C.FLAC__stream_encoder_delete(e.encoder)
		e.encoder = nil
	}
	if e.hEncoder != 0 {
		e.hEncoder.Delete()
		e.hEncoder = 0
	}
}

// GetFormat returns the encoder's configured audio format.
func (e *FlacEncoder) GetFormat() (sampleRate, channels, bitsPerSample int) {
	return e.sampleRate, e.channels, e.bitsPerSample
}

//export encoderWriteCallback
func encoderWriteCallback(
	encoder *C.FLAC__StreamEncoder,
	buffer *C.FLAC__byte,
	bytes C.size_t,
	samples C.uint32_t,
	currentFrame C.uint32_t,
	clientData unsafe.Pointer,
) C.FLAC__StreamEncoderWriteStatus {
	h := *(*cgo.Handle)(clientData)
	enc := h.Value().(*FlacEncoder)

	data := C.GoBytes(unsafe.Pointer(buffer), C.int(bytes))

	enc.mu.Lock()
	enc.outBuf = append(enc.outBuf, data...)
	enc.mu.Unlock()

	return C.FLAC__STREAM_ENCODER_WRITE_STATUS_OK
}

//export encoderMetadataCallback
func encoderMetadataCallback(
	encoder *C.FLAC__StreamEncoder,
	metadata *C.FLAC__StreamMetadata,
	clientData unsafe.Pointer,
) {
	h := *(*cgo.Handle)(clientData)
	enc := h.Value().(*FlacEncoder)

	if metadata._type != C.FLAC__METADATA_TYPE_STREAMINFO {
		return
	}

	// Extract the raw STREAMINFO data (34 bytes).
	// STREAMINFO layout: min_blocksize(2) + max_blocksize(2) + min_framesize(3)
	// + max_framesize(3) + sample_rate(20bits) + channels(3bits) + bps(5bits)
	// + total_samples(36bits) + md5(16bytes) = 34 bytes
	//
	// Use a FLAC__StreamMetadata iterator to serialize it properly.
	// For simplicity, reconstruct the 34 bytes from the struct fields.
	si := metadata.data // This is a C union — we need the stream_info member
	_ = si

	// Use libFLAC's metadata API to serialize the STREAMINFO block.
	// FLAC__metadata_object_clone + serialize would be ideal but the API
	// doesn't expose a direct serialize function for STREAMINFO.
	// Instead, capture key fields for the Go side.
	enc.streamInfo = make([]byte, 34)

	channels := int(C.get_decoder_channels(metadata))
	bps := int(C.get_decoder_depth(metadata))
	rate := int(C.get_decoder_rate(metadata))
	totalSamples := int64(C.get_total_samples(metadata))

	slog.Debug("Encoder STREAMINFO captured",
		"rate", rate, "channels", channels, "bps", bps, "totalSamples", totalSamples)

	// Serialize STREAMINFO per FLAC format spec:
	// Bytes 0-1: min blocksize (16-bit BE)
	// Bytes 2-3: max blocksize (16-bit BE)
	// Bytes 4-6: min framesize (24-bit BE)
	// Bytes 7-9: max framesize (24-bit BE)
	// Bytes 10-13: sample rate (20 bits) | channels-1 (3 bits) | bps-1 (5 bits) | total samples high 4 bits
	// Bytes 14-17: total samples low 32 bits
	// Bytes 18-33: MD5 signature (16 bytes)
	//
	// We read these from the C metadata struct.
	minBlock := int(C.get_min_blocksize(metadata))
	maxBlock := int(C.get_max_blocksize(metadata))
	minFrame := int(C.get_min_framesize(metadata))
	maxFrame := int(C.get_max_framesize(metadata))

	enc.streamInfo[0] = byte(minBlock >> 8)
	enc.streamInfo[1] = byte(minBlock)
	enc.streamInfo[2] = byte(maxBlock >> 8)
	enc.streamInfo[3] = byte(maxBlock)
	enc.streamInfo[4] = byte(minFrame >> 16)
	enc.streamInfo[5] = byte(minFrame >> 8)
	enc.streamInfo[6] = byte(minFrame)
	enc.streamInfo[7] = byte(maxFrame >> 16)
	enc.streamInfo[8] = byte(maxFrame >> 8)
	enc.streamInfo[9] = byte(maxFrame)

	// Pack sample rate (20 bits) + channels-1 (3 bits) + bps-1 (5 bits) + total samples high 4 bits
	packed := uint64(rate)<<44 |
		uint64(channels-1)<<41 |
		uint64(bps-1)<<36 |
		uint64(totalSamples)

	enc.streamInfo[10] = byte(packed >> 56)
	enc.streamInfo[11] = byte(packed >> 48)
	enc.streamInfo[12] = byte(packed >> 40)
	enc.streamInfo[13] = byte(packed >> 32)
	enc.streamInfo[14] = byte(packed >> 24)
	enc.streamInfo[15] = byte(packed >> 16)
	enc.streamInfo[16] = byte(packed >> 8)
	enc.streamInfo[17] = byte(packed)

	// Copy MD5 signature (16 bytes)
	C.get_md5_signature(metadata, (*C.uint8_t)(unsafe.Pointer(&enc.streamInfo[18])))
}

func getStreamEncoderInitStatusString(status C.FLAC__StreamEncoderInitStatus) string {
	var theCArray **C.char = (**C.char)(unsafe.Pointer(&C.FLAC__StreamEncoderInitStatusString))
	length := 14 // number of status strings
	slice := unsafe.Slice(theCArray, length)

	idx := int(status)
	if idx < 0 || idx >= length {
		return fmt.Sprintf("unknown status %d", idx)
	}
	return C.GoString(slice[idx])
}

// PCMToInt32 converts interleaved little-endian PCM bytes to int32 samples.
// This is a utility for converting raw PCM data to the format expected by
// ProcessInterleaved.
//
// Parameters:
//   - pcm: Raw PCM bytes (interleaved, little-endian)
//   - bitsPerSample: Bit depth (8, 16, 24, or 32)
//   - out: Output slice for int32 samples (must be large enough)
//
// Returns the number of samples written to out.
func PCMToInt32(pcm []byte, bitsPerSample int, out []int32) int {
	bytesPerSample := bitsPerSample / 8
	numSamples := len(pcm) / bytesPerSample
	if numSamples > len(out) {
		numSamples = len(out)
	}

	for i := 0; i < numSamples; i++ {
		off := i * bytesPerSample
		switch bytesPerSample {
		case 1:
			// 8-bit: signed (FLAC convention)
			out[i] = int32(int8(pcm[off]))
		case 2:
			// 16-bit: signed little-endian
			out[i] = int32(int16(pcm[off]) | int16(pcm[off+1])<<8)
		case 3:
			// 24-bit: signed little-endian
			v := int32(pcm[off]) | int32(pcm[off+1])<<8 | int32(pcm[off+2])<<16
			// Sign extend from 24-bit
			if v&0x800000 != 0 {
				v |= ^0xFFFFFF
			}
			out[i] = v
		case 4:
			// 32-bit: signed little-endian
			out[i] = int32(pcm[off]) | int32(pcm[off+1])<<8 | int32(pcm[off+2])<<16 | int32(pcm[off+3])<<24
		}
	}

	return numSamples
}
