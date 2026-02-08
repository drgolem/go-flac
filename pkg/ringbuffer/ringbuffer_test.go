package ringbuffer

import (
	"sync"
	"testing"
)

func TestNewRingBuffer(t *testing.T) {
	tests := []struct {
		name             string
		capacity         int
		expectedCapacity int
	}{
		{"power of 2", 1024, 1024},
		{"not power of 2", 1000, 1024},
		{"small", 10, 16},
		{"one", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := NewRingBuffer(tt.capacity)
			if rb.Capacity() != tt.expectedCapacity {
				t.Errorf("expected capacity %d, got %d", tt.expectedCapacity, rb.Capacity())
			}
			if !rb.IsEmpty() {
				t.Error("new buffer should be empty")
			}
		})
	}
}

func TestWriteRead(t *testing.T) {
	rb := NewRingBuffer(1024)

	// Write some data
	data := []byte("hello world")
	n, err := rb.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected to write %d bytes, wrote %d", len(data), n)
	}

	// Check size
	if rb.Size() != len(data) {
		t.Errorf("expected size %d, got %d", len(data), rb.Size())
	}

	// Read data back
	dst := make([]byte, len(data))
	n, err = rb.Read(len(data), dst)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected to read %d bytes, read %d", len(data), n)
	}

	// Verify data
	if string(dst) != string(data) {
		t.Errorf("expected %q, got %q", string(data), string(dst))
	}

	// Buffer should be empty
	if !rb.IsEmpty() {
		t.Error("buffer should be empty after reading all data")
	}
}

func TestWrapAround(t *testing.T) {
	rb := NewRingBuffer(16) // Will be rounded to 16

	// Write data that will cause wrap-around
	data1 := []byte("1234567890") // 10 bytes
	_, err := rb.Write(data1)
	if err != nil {
		t.Fatalf("Write1 failed: %v", err)
	}

	// Read some data
	dst := make([]byte, 5)
	_, err = rb.Read(5, dst)
	if err != nil {
		t.Fatalf("Read1 failed: %v", err)
	}

	// Write more data (should wrap around)
	data2 := []byte("ABCDEFGHIJ") // 10 bytes
	_, err = rb.Write(data2)
	if err != nil {
		t.Fatalf("Write2 failed: %v", err)
	}

	// Read all remaining data
	dst = make([]byte, 15)
	n, err := rb.Read(15, dst)
	if err != nil {
		t.Fatalf("Read2 failed: %v", err)
	}

	expected := "67890ABCDEFGHIJ"
	if string(dst[:n]) != expected {
		t.Errorf("expected %q, got %q", expected, string(dst[:n]))
	}
}

func TestOverflow(t *testing.T) {
	rb := NewRingBuffer(16)

	// Try to write more than capacity
	data := make([]byte, 20)
	_, err := rb.Write(data)
	if err == nil {
		t.Error("expected error when writing more than capacity")
	}

	// Fill buffer
	data = make([]byte, 16)
	_, err = rb.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Try to write when full
	_, err = rb.Write([]byte{1})
	if err == nil {
		t.Error("expected error when writing to full buffer")
	}
}

func TestUnderflow(t *testing.T) {
	rb := NewRingBuffer(16)

	// Try to read from empty buffer
	dst := make([]byte, 10)
	_, err := rb.Read(10, dst)
	if err == nil {
		t.Error("expected error when reading from empty buffer")
	}

	// Write less than requested read
	rb.Write([]byte("hello"))

	_, err = rb.Read(10, dst)
	if err == nil {
		t.Error("expected error when reading more than available")
	}
}

func TestReset(t *testing.T) {
	rb := NewRingBuffer(16)

	// Write and read some data
	rb.Write([]byte("test"))
	rb.Reset()

	if !rb.IsEmpty() {
		t.Error("buffer should be empty after reset")
	}
	if rb.Size() != 0 {
		t.Errorf("expected size 0 after reset, got %d", rb.Size())
	}
}

func TestConcurrentAccess(t *testing.T) {
	rb := NewRingBuffer(65536)

	const iterations = 10000
	const chunkSize = 64

	var wg sync.WaitGroup
	wg.Add(2)

	// Producer goroutine
	go func() {
		defer wg.Done()
		data := make([]byte, chunkSize)
		for i := 0; i < chunkSize; i++ {
			data[i] = byte(i)
		}

		for i := 0; i < iterations; i++ {
			for {
				_, err := rb.Write(data)
				if err == nil {
					break
				}
				// Buffer full, wait a bit
			}
		}
	}()

	// Consumer goroutine
	go func() {
		defer wg.Done()
		dst := make([]byte, chunkSize)
		totalRead := 0

		for totalRead < iterations*chunkSize {
			n, err := rb.Read(chunkSize, dst)
			if err == nil {
				totalRead += n

				// Verify data pattern
				for i := 0; i < n; i++ {
					if dst[i] != byte(i) {
						t.Errorf("data corruption at offset %d: expected %d, got %d",
							totalRead-n+i, byte(i), dst[i])
						return
					}
				}
			}
			// Buffer empty, continue trying
		}
	}()

	wg.Wait()

	// Buffer should be empty at the end
	if !rb.IsEmpty() {
		t.Errorf("buffer should be empty, but has %d bytes", rb.Size())
	}
}

func TestNextPowerOf2(t *testing.T) {
	tests := []struct {
		input    uint64
		expected uint64
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{7, 8},
		{8, 8},
		{9, 16},
		{1000, 1024},
		{1024, 1024},
		{1025, 2048},
	}

	for _, tt := range tests {
		result := nextPowerOf2(tt.input)
		if result != tt.expected {
			t.Errorf("nextPowerOf2(%d) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}

func BenchmarkWrite(b *testing.B) {
	rb := NewRingBuffer(65536)
	data := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Write(data)
		if rb.IsFull() {
			rb.Reset()
		}
	}
}

func BenchmarkRead(b *testing.B) {
	rb := NewRingBuffer(65536)
	data := make([]byte, 1024)
	dst := make([]byte, 1024)

	// Fill buffer
	for !rb.IsFull() {
		rb.Write(data)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Read(1024, dst)
		if rb.IsEmpty() {
			// Refill
			for !rb.IsFull() {
				rb.Write(data)
			}
		}
	}
}

func BenchmarkWriteReadPair(b *testing.B) {
	rb := NewRingBuffer(65536)
	data := make([]byte, 1024)
	dst := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Write(data)
		rb.Read(1024, dst)
	}
}
