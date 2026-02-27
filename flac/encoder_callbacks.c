#include "FLAC/stream_encoder.h"
#include "FLAC/metadata.h"

#include "_cgo_export.h"

/* Write callback wrapper for init_stream mode. Casts const away for Go. */
FLAC__StreamEncoderWriteStatus
encoderWriteCallback_cgo(const FLAC__StreamEncoder *encoder,
                         const FLAC__byte buffer[],
                         size_t bytes,
                         uint32_t samples,
                         uint32_t current_frame,
                         void *client_data)
{
    return encoderWriteCallback(
        (FLAC__StreamEncoder *)encoder,
        (FLAC__byte *)buffer,
        bytes, samples, current_frame,
        client_data);
}

/* Metadata callback wrapper for init_stream mode.
 * Called when encoder finishes with updated STREAMINFO. */
void
encoderMetadataCallback_cgo(const FLAC__StreamEncoder *encoder,
                            const FLAC__StreamMetadata *metadata,
                            void *client_data)
{
    encoderMetadataCallback(
        (FLAC__StreamEncoder *)encoder,
        (FLAC__StreamMetadata *)metadata,
        client_data);
}
