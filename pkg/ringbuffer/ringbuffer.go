// Package ringbuffer provides a lock-free SPSC (Single Producer Single Consumer) ring buffer.
//
// This implementation is optimized for scenarios where one goroutine writes (producer)
// and another goroutine reads (consumer), such as CGO callbacks writing audio data
// that Go code reads. It uses atomic operations to achieve thread-safety without locks.
//
// IMPORTANT: This ring buffer is ONLY safe for single producer + single consumer.
// Multiple producers or multiple consumers will cause data races.
package ringbuffer

import (
	"fmt"
	"sync/atomic"
)

// RingBuffer is a lock-free SPSC (Single Producer Single Consumer) circular buffer.
// It is safe for concurrent access by exactly one producer and one consumer.
//
// Thread-safety guarantees:
//   - Producer calls: Write() - safe from one goroutine
//   - Consumer calls: Read(), Size(), IsEmpty() - safe from another goroutine
//   - Shared calls: Capacity(), Reset() - NOT thread-safe, call when idle
type RingBuffer struct {
	buf []byte

	// Atomic fields must be 64-bit aligned for 32-bit systems
	// readPos is only modified by consumer
	readPos uint64

	// writePos is only modified by producer
	writePos uint64

	// capacity is the buffer size (power of 2 for efficient modulo)
	capacity uint64

	// mask is capacity - 1, used for fast modulo via bitwise AND
	mask uint64
}

// NewRingBuffer creates a new lock-free SPSC ring buffer with the specified capacity.
// The capacity will be rounded up to the next power of 2 for efficient modulo operations.
//
// Panics if capacity <= 0.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		panic("capacity must be positive")
	}

	// Round up to next power of 2 for efficient modulo
	actualCapacity := nextPowerOf2(uint64(capacity))

	rb := &RingBuffer{
		buf:      make([]byte, actualCapacity),
		capacity: actualCapacity,
		mask:     actualCapacity - 1,
	}

	return rb
}

// nextPowerOf2 returns the next power of 2 >= n
func nextPowerOf2(n uint64) uint64 {
	if n == 0 {
		return 1
	}

	// If already power of 2, return as-is
	if n&(n-1) == 0 {
		return n
	}

	// Round up to next power of 2
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	n++

	return n
}

// Capacity returns the buffer capacity. Safe to call concurrently.
func (rb *RingBuffer) Capacity() int {
	return int(rb.capacity)
}

// Size returns the current number of bytes in the buffer.
// Safe to call from consumer goroutine.
func (rb *RingBuffer) Size() int {
	// Load positions atomically
	writePos := atomic.LoadUint64(&rb.writePos)
	readPos := atomic.LoadUint64(&rb.readPos)

	// Calculate size (handles wrap-around)
	return int(writePos - readPos)
}

// AvailableWriteSize returns available space for writing.
// Safe to call from producer goroutine.
func (rb *RingBuffer) AvailableWriteSize() int {
	return int(rb.capacity) - rb.Size()
}

// IsEmpty returns true if the buffer is empty.
// Safe to call from consumer goroutine.
func (rb *RingBuffer) IsEmpty() bool {
	writePos := atomic.LoadUint64(&rb.writePos)
	readPos := atomic.LoadUint64(&rb.readPos)
	return writePos == readPos
}

// IsFull returns true if the buffer is full.
// Safe to call from producer goroutine.
func (rb *RingBuffer) IsFull() bool {
	return rb.Size() >= int(rb.capacity)
}

// Reset clears the buffer.
// NOT thread-safe - only call when producer and consumer are idle.
func (rb *RingBuffer) Reset() {
	atomic.StoreUint64(&rb.readPos, 0)
	atomic.StoreUint64(&rb.writePos, 0)
}

// Read reads up to n bytes from the buffer into dst.
// Returns the number of bytes read and an error if insufficient data.
//
// MUST be called from consumer goroutine only.
// Thread-safe when paired with Write() from producer goroutine.
func (rb *RingBuffer) Read(n int, dst []byte) (int, error) {
	if len(dst) < n {
		return 0, fmt.Errorf("destination buffer too small: %d < %d", len(dst), n)
	}

	// Load positions
	writePos := atomic.LoadUint64(&rb.writePos)
	readPos := atomic.LoadUint64(&rb.readPos)

	// Check available data
	available := int(writePos - readPos)
	if available < n {
		return 0, fmt.Errorf("insufficient data: available=%d, requested=%d", available, n)
	}

	// Calculate actual positions in buffer using mask
	readIdx := readPos & rb.mask

	// Check if read wraps around
	if readIdx+uint64(n) <= rb.capacity {
		// No wrap-around, single copy
		copy(dst, rb.buf[readIdx:readIdx+uint64(n)])
	} else {
		// Wrap-around, two copies
		firstChunk := int(rb.capacity - readIdx)
		copy(dst, rb.buf[readIdx:rb.capacity])
		copy(dst[firstChunk:], rb.buf[0:n-firstChunk])
	}

	// Update read position atomically (only consumer modifies this)
	atomic.StoreUint64(&rb.readPos, readPos+uint64(n))

	return n, nil
}

// Write writes data to the buffer.
// Returns the number of bytes written and an error if insufficient space.
//
// MUST be called from producer goroutine only.
// Thread-safe when paired with Read() from consumer goroutine.
func (rb *RingBuffer) Write(data []byte) (int, error) {
	dataLen := len(data)
	if dataLen == 0 {
		return 0, nil
	}

	// Load positions
	writePos := atomic.LoadUint64(&rb.writePos)
	readPos := atomic.LoadUint64(&rb.readPos)

	// Check available space
	used := int(writePos - readPos)
	available := int(rb.capacity) - used

	if dataLen > available {
		return 0, fmt.Errorf("insufficient space: available=%d, requested=%d", available, dataLen)
	}

	// Calculate actual position in buffer using mask
	writeIdx := writePos & rb.mask

	// Check if write wraps around
	if writeIdx+uint64(dataLen) <= rb.capacity {
		// No wrap-around, single copy
		copy(rb.buf[writeIdx:], data)
	} else {
		// Wrap-around, two copies
		firstChunk := int(rb.capacity - writeIdx)
		copy(rb.buf[writeIdx:], data[:firstChunk])
		copy(rb.buf[0:], data[firstChunk:])
	}

	// Update write position atomically (only producer modifies this)
	// Memory barrier ensures writes to buffer happen before position update
	atomic.StoreUint64(&rb.writePos, writePos+uint64(dataLen))

	return dataLen, nil
}
