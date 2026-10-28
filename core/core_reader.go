package core

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
)

// Seek mirrors io.Seeker.
func (cursor *readerAtCursor) Seek(offset int64, whence int) (int64, error) {
	var position int64
	switch whence {
	case io.SeekStart:
		position = offset
	case io.SeekCurrent:
		position = cursor.GetFileOffset() + offset
	case io.SeekEnd:
		position = cursor.size + offset
	default:
		return 0, fmt.Errorf("readerAtCursor.Seek: invalid whence %d", whence)
	}
	if position < 0 {
		return 0, errors.New("readerAtCursor.Seek: negative position")
	}
	cursor.SetFileOffset(position)
	return position, nil
}

// Close closes the underlying source if it is an io.Closer.
func (cursor *readerAtCursor) Close() error {
	if closer, ok := cursor.source.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Read implements io.Reader.
func (cursor *readerAtCursor) Read(p []byte) (int, error) { return cursor.buffered.Read(p) }

func (reader *offsetReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("offsetReaderAt.ReadAt: negative offset")
	}
	return reader.source.ReadAt(p, off+reader.offset)
}

type offsetReaderAt struct {
	source io.ReaderAt
	offset int64
}

func (parser *PdfParser) newCursor(offset int64) *readerAtCursor {
	return newReaderAtCursor(parser.reader, parser.fileSize, offset)
}

// ReadAtLeast reads at least n bytes into p.
func (cursor *readerAtCursor) ReadAtLeast(p []byte, n int) (int, error) {
	remaining := n
	total := 0
	for remaining > 0 {
		nr, err := cursor.Read(p[total:])
		if err != nil {
			return total, err
		}
		total += nr
		remaining -= nr
	}
	return total, nil
}

// Discard skips the next n bytes.
func (cursor *readerAtCursor) Discard(n int) (int, error) { return cursor.buffered.Discard(n) }

// Peek returns the next n bytes without advancing the cursor.
func (cursor *readerAtCursor) Peek(n int) ([]byte, error) { return cursor.buffered.Peek(n) }

type readerAtCursor struct {
	source        io.ReaderAt
	size          int64
	sectionOffset int64
	section       *io.SectionReader
	buffered      *bufio.Reader
}

// ReadBytes reads until the first occurrence of delim.
func (cursor *readerAtCursor) ReadBytes(delim byte) ([]byte, error) {
	return cursor.buffered.ReadBytes(delim)
}

// ReadAt exposes the underlying io.ReaderAt so callers that don't need cursor
// state (e.g. ReadBytesAt) can read directly. Does not advance the cursor.
func (cursor *readerAtCursor) ReadAt(p []byte, off int64) (int, error) {
	return cursor.source.ReadAt(p, off)
}

// SizeOfReaderAt auto-detects the byte length of an io.ReaderAt source by
// type-asserting for the common ways a source carries its size:
//   - interface{ Size() int64 } — *bytes.Reader, *io.SectionReader
//   - interface{ Stat() (os.FileInfo, error) } — *os.File
//   - io.Seeker — fallback for other seekable sources
//
// Stat is checked before Seek because *os.File satisfies both, and Stat is
// side-effect-free whereas Seek mutates the file position.
//
// Returns an error if the source supports none of these. Callers with such
// sources should use the explicit-size constructor (e.g. model.NewPdfReaderAt)
// and pass the size from their own knowledge.
func SizeOfReaderAt(r io.ReaderAt) (int64, error) {
	if r == nil {
		return 0, errors.New("core.SizeOfReaderAt: nil source")
	}
	if iface, ok := r.(interface{ Size() int64 }); ok {
		return iface.Size(), nil
	}
	if iface, ok := r.(interface{ Stat() (os.FileInfo, error) }); ok {
		info, err := iface.Stat()
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	}
	if seeker, ok := r.(io.Seeker); ok {
		size, err := seeker.Seek(0, io.SeekEnd)
		if err != nil {
			return 0, err
		}
		if _, err2 := seeker.Seek(0, io.SeekStart); err2 != nil {
			return 0, err2
		}
		return size, nil
	}
	return 0, errors.New("core.SizeOfReaderAt: source does not support Size(), Stat(), or Seek(); use the explicit-size constructor")
}

// GetFileOffset returns the file offset of the next byte to be consumed.
// It is derived from the section reader's position (bytes pulled into the
// buffer) minus the bytes still buffered, so it stays accurate regardless of
// whether bytes were consumed through the cursor methods or directly via the
// buffer returned by Underlying().
func (cursor *readerAtCursor) GetFileOffset() int64 {
	sectionPosition, _ := cursor.section.Seek(0, io.SeekCurrent)
	return cursor.sectionOffset + sectionPosition - int64(cursor.buffered.Buffered())
}

// ReadByte reads a single byte.
func (cursor *readerAtCursor) ReadByte() (byte, error) { return cursor.buffered.ReadByte() }

// Underlying returns the inner *bufio.Reader so helpers that take a raw
// *bufio.Reader (ParseNumber) keep working without copying bytes. Bytes
// consumed through it are accounted for by GetFileOffset.
func (cursor *readerAtCursor) Underlying() *bufio.Reader { return cursor.buffered }

// UnreadByte unreads the last byte read.
func (cursor *readerAtCursor) UnreadByte() error { return cursor.buffered.UnreadByte() }

// Buffered returns the number of bytes currently in the buffer.
func (cursor *readerAtCursor) Buffered() int { return cursor.buffered.Buffered() }

func newReaderAtCursor(source io.ReaderAt, size, offset int64) *readerAtCursor {
	if offset < 0 {
		offset = 0
	}
	cursor := &readerAtCursor{source: source, size: size}
	cursor.reset(offset)
	return cursor
}

// SetFileOffset repositions the cursor and reinitialises the buffer.
func (cursor *readerAtCursor) SetFileOffset(offset int64) {
	if offset < 0 {
		offset = 0
	}
	cursor.reset(offset)
}

func newOffsetReaderAt(source io.ReaderAt, offset int64) *offsetReaderAt {
	return &offsetReaderAt{source: source, offset: offset}
}

func (cursor *readerAtCursor) reset(offset int64) {
	cursor.sectionOffset = offset
	cursor.section = io.NewSectionReader(cursor.source, offset, cursor.size-offset)
	cursor.buffered = bufio.NewReader(cursor.section)
}
