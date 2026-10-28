package core

import (
	"bytes"
	"sync"
)

var bytePoolSizes = [...]int{poolSize16K, poolSize64K, poolSize256K, poolSize1M, poolSize4M, poolSize16M}

const minPoolBufSize = poolSize16K / 4

func releaseBuffer(buffer *bytes.Buffer) {
	if buffer.Cap() > poolSize16M {
		return
	}
	buffer.Reset()
	bufferPool.Put(buffer)
}

func poolIndexFor(size int) int {
	if size < minPoolBufSize {
		return -1
	}
	for i, bytePoolSize := range bytePoolSizes {
		if size <= bytePoolSize {
			return i
		}
	}
	return -1
}

const (
	poolSize16K  = 16 << 10
	poolSize64K  = 64 << 10
	poolSize256K = 256 << 10
	poolSize1M   = 1 << 20
	poolSize4M   = 4 << 20
	poolSize16M  = 16 << 20
)

// AcquireBytes returns a []byte with cap at least size, drawn from a pooled
// size-class bucket. The returned slice has len == size. If size exceeds the
// largest bucket, a plain make([]byte, size) is returned instead; that buffer
// is still safe to pass to ReleaseBytes (it is dropped because its cap does
// not match a bucket). If size <= 0, returns nil.
func AcquireBytes(size int) []byte {
	if size <= 0 {
		return nil
	}
	poolIdx := poolIndexFor(size)
	if poolIdx < 0 {
		return make([]byte, size)
	}
	data := bytePools[poolIdx].Get().(*[]byte)
	return (*data)[:size]
}

func withPooledBuffer(fn func(*bytes.Buffer)) []byte {
	buffer := acquireBuffer()
	defer releaseBuffer(buffer)
	fn(buffer)
	data := buffer.Bytes()
	out := make([]byte, len(data))
	copy(out, data)
	return out
}

var bufferPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func acquireBuffer() *bytes.Buffer { return bufferPool.Get().(*bytes.Buffer) }

var bytePools = [len(bytePoolSizes)]sync.Pool{{New: func() any { data := make([]byte, poolSize16K); return &data }}, {New: func() any { data := make([]byte, poolSize64K); return &data }}, {New: func() any { data := make([]byte, poolSize256K); return &data }}, {New: func() any {
	data := make([]byte, poolSize1M)
	return &data
}}, {New: func() any { data := make([]byte, poolSize4M); return &data }}, {New: func() any { data := make([]byte, poolSize16M); return &data }}}

// AcquireBytesZeroed is AcquireBytes with a guarantee that buf[:size] is all
// zeroes. Capacity beyond len is not cleared, so a reslice or append past len
// can still expose bytes from a previous user of the buffer. It is for callers
// that read the buffer before writing every byte of it (e.g. accumulating into
// an alpha mask); callers that overwrite the whole buffer should use
// AcquireBytes and skip the clear. Only pooled buffers are cleared: the make()
// fallback for sizes outside the buckets is already zeroed by the runtime, and
// clearing it would touch every page for nothing.
func AcquireBytesZeroed(size int) []byte {
	bytes := AcquireBytes(size)
	if poolIndexFor(size) >= 0 {
		clear(bytes)
	}
	return bytes
}

func isPoolSize(n int) bool {
	for _, bytePoolSize := range bytePoolSizes {
		if n == bytePoolSize {
			return true
		}
	}
	return false
}

// UnpooledBytes returns buf, or a copy of it when ReleaseBytes would take
// buf's capacity into the pool. ReleaseBytes never pools the result, so it can
// be shared by values whose owners may each release it.
func UnpooledBytes(buf []byte) []byte {
	if !isPoolSize(cap(buf)) {
		return buf
	}
	size := len(buf)
	for isPoolSize(size) {
		size++
	}
	data := make([]byte, len(buf), size)
	copy(data, buf)
	return data
}

// ReleaseBytes returns buf to the pool bucket whose capacity matches cap(buf).
// Buffers whose cap does not match any bucket (including nil and empty
// slices) are dropped silently, so this is safe to call with any []byte.
func ReleaseBytes(buf []byte) {
	c := cap(buf)
	if c == 0 {
		return
	}
	for i, bytePoolSize := range bytePoolSizes {
		if c == bytePoolSize {
			data := buf[:c]
			bytePools[i].Put(&data)
			return
		}
	}
}
