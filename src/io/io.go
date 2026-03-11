// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package io 提供了 I/O 基本操作的基础接口。
// 其主要工作是将这些基本操作的现有实现（例如 os 包中的实现）包装成共享的公共接口，以抽象出功能，并提供一些其他相关的基本操作。
//
// 由于这些接口和基本操作封装了具有不同实现的低级操作，除非另有说明，否则客户端不应假定它们可以安全地并行执行。
package io

import (
	"errors"
	"sync"
)

// Seek 操作的定位基准值。
const (
	SeekStart   = 0 // 相对于文件起始位置进行定位
	SeekCurrent = 1 // 相对于当前位置进行定位
	SeekEnd     = 2 // 相对于文件末尾进行定位
)

// ErrShortWrite 表示写入操作接收的字节数少于请求的字节数，
// 但未能返回一个明确的错误。
var ErrShortWrite = errors.New("short write")

// errInvalidWrite 表示写入操作返回了一个不可能的计数。
var errInvalidWrite = errors.New("invalid write result")

// ErrShortBuffer 表示读取操作需要的缓冲区长度大于提供的长度。
var ErrShortBuffer = errors.New("short buffer")

// EOF 是当没有更多输入可用时 Read 返回的错误。
// (Read 必须返回 EOF 本身，而不是包装了 EOF 的错误，因为调用方会使用 == 来测试 EOF。)
// 函数应仅在指示输入正常结束时返回 EOF。
// 如果在结构化数据流中意外遇到 EOF，则适当的错误是 [ErrUnexpectedEOF] 或其他能提供更详细信息的错误。
var EOF = errors.New("EOF")

// ErrUnexpectedEOF 表示在读取固定大小的块或数据结构过程中遇到了 EOF。
var ErrUnexpectedEOF = errors.New("unexpected EOF")

// ErrNoProgress 是 [Reader] 的某些客户端在多次调用 Read 都未能返回任何数据或错误时返回的错误，通常表明 [Reader] 的实现存在问题。
var ErrNoProgress = errors.New("multiple Read calls return no data or error")

// Reader 是包装了基本 Read 方法的接口。
//
// Read 读取最多 len(p) 个字节到 p 中。它返回读取的字节数 (0 <= n <= len(p)) 和遇到的任何错误。即使 Read 返回 n < len(p)，它也可能在调用期间将 p 的全部作为临时空间使用。
// 如果有数据可用但不足 len(p) 个字节，Read 通常会返回当前可用的数据，而不是等待更多数据。
//
// 当 Read 在成功读取 n > 0 个字节后遇到错误或文件结束条件时，它会返回已读取的字节数。它可能在同一调用中返回（非 nil 的）错误，或者在后续调用中返回错误（且 n == 0）。
// 这种一般情况的一个实例是，在输入流末尾返回非零字节数的 Reader 可能返回 err  EOF 或 err  nil。下一次 Read 应返回 0, EOF。
//
// 调用方在处理错误 err 之前，应始终先处理返回的 n > 0 个字节。这样做可以正确处理读取部分字节后发生的 I/O 错误，以及两种允许的 EOF 行为。
//
// 如果 len(p)  0，Read 应始终返回 n  0。如果已知某些错误条件（例如 EOF），它可能会返回非 nil 的错误。
//
// 不鼓励 Read 的实现返回零字节计数且错误为 nil，除非 len(p) == 0。调用方应将返回 0 和 nil 视为未发生任何操作；特别是这不表示 EOF。
//
// 实现不得保留 p。
type Reader interface {
	Read(p []byte) (n int, err error)
}

// Writer 是包装了基本 Write 方法的接口。
//
// Write 从 p 中写入 len(p) 个字节到底层数据流。它返回从 p 写入的字节数 (0 <= n <= len(p))
// 以及导致写入提前停止的任何错误。如果返回 n < len(p)，Write 必须返回一个非 nil 的错误。
// Write 不得修改切片数据，即使是临时修改。
//
// 实现不得保留 p。
type Writer interface {
	Write(p []byte) (n int, err error)
}

// Closer 是包装了基本 Close 方法的接口。
//
// 首次调用 Close 之后的行为是未定义的。具体实现可能会记录它们自身的行为。
type Closer interface {
	Close() error
}

// Seeker 是包装了基本 Seek 方法的接口。
//
// Seek 将下一次读取或写入的偏移量设置为 offset，其解释取决于 whence：
// [SeekStart] 表示相对于文件起始位置，
// [SeekCurrent] 表示相对于当前位置，
// [SeekEnd] 表示相对于文件末尾
// （例如，offset = -2 表示文件的倒数第二个字节）。
// Seek 返回相对于文件起始位置的新偏移量，如果出错则返回错误。
//
// 定位到文件起始位置之前是一个错误。
// 定位到任何正偏移量可能是允许的，但如果新的偏移量超过了底层对象的大小，后续 I/O 操作的行为是依赖于具体实现的。
type Seeker interface {
	Seek(offset int64, whence int) (int64, error)
}

// ReadWriter 是将基本的 Read 和 Write 方法组合在一起的接口。
type ReadWriter interface {
	Reader
	Writer
}

// ReadCloser 是将基本的 Read 和 Close 方法组合在一起的接口。
type ReadCloser interface {
	Reader
	Closer
}

// WriteCloser 是将基本的 Write 和 Close 方法组合在一起的接口。
type WriteCloser interface {
	Writer
	Closer
}

// ReadWriteCloser 是将基本的 Read、Write 和 Close 方法组合在一起的接口。
type ReadWriteCloser interface {
	Reader
	Writer
	Closer
}

// ReadSeeker 是将基本的 Read 和 Seek 方法组合在一起的接口。
type ReadSeeker interface {
	Reader
	Seeker
}

// ReadSeekCloser 是将基本的 Read、Seek 和 Close 方法组合在一起的接口。
type ReadSeekCloser interface {
	Reader
	Seeker
	Closer
}

// WriteSeeker 是将基本的 Write 和 Seek 方法组合在一起的接口。
type WriteSeeker interface {
	Writer
	Seeker
}

// ReadWriteSeeker 是将基本的 Read、Write 和 Seek 方法组合在一起的接口。
type ReadWriteSeeker interface {
	Reader
	Writer
	Seeker
}

// ReaderFrom 是包装了 ReadFrom 方法的接口。
//
// ReadFrom 从 r 读取数据，直到遇到 EOF 或错误。返回值 n 是读取的字节数。读取过程中遇到的任何错误（除 EOF 外）也会被返回。
//
// 如果可用，[Copy] 函数会使用 [ReaderFrom]。
type ReaderFrom interface {
	ReadFrom(r Reader) (n int64, err error)
}

// WriterTo 是包装了 WriteTo 方法的接口。
//
// WriteTo 将数据写入 w，直到没有更多数据可写或发生错误。返回值 n 是写入的字节数。写入过程中遇到的任何错误也会被返回。
//
// Copy 函数在可用时会使用 WriterTo。
type WriterTo interface {
	WriteTo(w Writer) (n int64, err error)
}

// ReaderAt 是包装了基本 ReadAt 方法的接口。
//
// ReadAt 从底层输入源的偏移量 off 处开始，读取最多 len(p) 个字节到 p 中。它返回读取的字节数 (0 <= n <= len(p)) 和遇到的任何错误。
//
// 当 ReadAt 返回 n < len(p) 时，它会返回一个非 nil 的错误来解释为何没有返回更多字节。在这方面，ReadAt 比 Read 更严格。
//
// 即使 ReadAt 返回 n < len(p)，它也可能在调用期间将 p 的全部作为临时空间使用。如果有一些数据可用但不足 len(p) 个字节，
// ReadAt 会阻塞直到所有数据都可用或发生错误为止。在这方面 ReadAt 与 Read 不同。
//
// 如果 ReadAt 返回的 n = len(p) 个字节位于输入源的末尾，ReadAt 可能返回 err  EOF 或 err  nil。
//
// 如果 ReadAt 正在从具有查找偏移量的输入源进行读取，ReadAt 不应影响底层的查找偏移量，也不应受其影响。
//
// ReadAt 的客户端可以在同一输入源上并行执行 ReadAt 调用。
//
// 实现不得保留 p。
type ReaderAt interface {
	ReadAt(p []byte, off int64) (n int, err error)
}

// WriterAt 是包装了基本 WriteAt 方法的接口。
//
// WriteAt 从 p 中写入 len(p) 个字节到底层数据流的偏移量 off 处。它返回从 p 写入的字节数 (0 <= n <= len(p))
// 以及导致写入提前停止的任何错误。如果返回 n < len(p)，WriteAt 必须返回一个非 nil 的错误。
//
// 如果 WriteAt 正在写入具有查找偏移量的目标，WriteAt 不应影响底层的查找偏移量，也不应受其影响。
//
// 如果范围不重叠，WriteAt 的客户端可以在同一目标上并行执行 WriteAt 调用。
//
// 实现不得保留 p。
type WriterAt interface {
	WriteAt(p []byte, off int64) (n int, err error)
}

// ByteReader 是包装了 ReadByte 方法的接口。
//
// ReadByte 从输入中读取并返回下一个字节，或返回遇到的任何错误。如果 ReadByte 返回错误，则没有消耗任何输入字节，且返回的字节值是未定义的。
//
// ReadByte 为逐字节处理提供了一个高效的接口。未实现 ByteReader 的 [Reader] 可以使用 bufio.NewReader 包装来添加此方法。
type ByteReader interface {
	ReadByte() (byte, error)
}

// ByteScanner 是在基本的 ReadByte 方法上增加了 UnreadByte 方法的接口。
//
// UnreadByte 会导致下一次对 ReadByte 的调用返回最后一次读取的字节。
// 如果最后一次操作不是一次成功的 ReadByte 调用，UnreadByte 可能返回一个错误、回退最后一次读取的字节（或上次未读取字节之前的字节），
// 或者（在支持 [Seeker] 接口的实现中）定位到当前偏移量之前一个字节的位置。
type ByteScanner interface {
	ByteReader
	UnreadByte() error
}

// ByteWriter 是包装了 WriteByte 方法的接口。
type ByteWriter interface {
	WriteByte(c byte) error
}

// RuneReader 是包装了 ReadRune 方法的接口。
//
// ReadRune 读取一个编码的 Unicode 字符，并返回该字符及其占用的字节数。如果没有可用的字符，err 将被设置。
type RuneReader interface {
	ReadRune() (r rune, size int, err error)
}

// RuneScanner 是在基本的 ReadRune 方法上增加了 UnreadRune 方法的接口。
//
// UnreadRune 会导致下一次对 ReadRune 的调用返回最后一次读取的字符。
// 如果最后一次操作不是一次成功的 ReadRune 调用，UnreadRune 可能返回一个错误、
// 回退最后一次读取的字符（或上次未读取字符之前的字符），或者（在支持 [Seeker] 接口的实现中）定位到当前偏移量之前该字符的起始位置。
type RuneScanner interface {
	RuneReader
	UnreadRune() error
}

// StringWriter 是包装了 WriteString 方法的接口。
type StringWriter interface {
	WriteString(s string) (n int, err error)
}

// WriteString writes the contents of the string s to w, which accepts a slice of bytes.
// If w implements [StringWriter], [StringWriter.WriteString] is invoked directly.
// Otherwise, [Writer.Write] is called exactly once.
func WriteString(w Writer, s string) (n int, err error) {
	if sw, ok := w.(StringWriter); ok {
		return sw.WriteString(s)
	}
	return w.Write([]byte(s))
}

// ReadAtLeast reads from r into buf until it has read at least min bytes.
// It returns the number of bytes copied and an error if fewer bytes were read.
// The error is EOF only if no bytes were read.
// If an EOF happens after reading fewer than min bytes,
// ReadAtLeast returns [ErrUnexpectedEOF].
// If min is greater than the length of buf, ReadAtLeast returns [ErrShortBuffer].
// On return, n >= min if and only if err == nil.
// If r returns an error having read at least min bytes, the error is dropped.
func ReadAtLeast(r Reader, buf []byte, min int) (n int, err error) {
	if len(buf) < min {
		return 0, ErrShortBuffer
	}
	for n < min && err == nil {
		var nn int
		nn, err = r.Read(buf[n:])
		n += nn
	}
	if n >= min {
		err = nil
	} else if n > 0 && err == EOF {
		err = ErrUnexpectedEOF
	}
	return
}

// ReadFull reads exactly len(buf) bytes from r into buf.
// It returns the number of bytes copied and an error if fewer bytes were read.
// The error is EOF only if no bytes were read.
// If an EOF happens after reading some but not all the bytes,
// ReadFull returns [ErrUnexpectedEOF].
// On return, n == len(buf) if and only if err == nil.
// If r returns an error having read at least len(buf) bytes, the error is dropped.
func ReadFull(r Reader, buf []byte) (n int, err error) {
	return ReadAtLeast(r, buf, len(buf))
}

// CopyN copies n bytes (or until an error) from src to dst.
// It returns the number of bytes copied and the earliest
// error encountered while copying.
// On return, written == n if and only if err == nil.
//
// If dst implements [ReaderFrom], the copy is implemented using it.
func CopyN(dst Writer, src Reader, n int64) (written int64, err error) {
	written, err = Copy(dst, LimitReader(src, n))
	if written == n {
		return n, nil
	}
	if written < n && err == nil {
		// src stopped early; must have been EOF.
		err = EOF
	}
	return
}

// Copy copies from src to dst until either EOF is reached
// on src or an error occurs. It returns the number of bytes
// copied and the first error encountered while copying, if any.
//
// A successful Copy returns err == nil, not err == EOF.
// Because Copy is defined to read from src until EOF, it does
// not treat an EOF from Read as an error to be reported.
//
// If src implements [WriterTo],
// the copy is implemented by calling src.WriteTo(dst).
// Otherwise, if dst implements [ReaderFrom],
// the copy is implemented by calling dst.ReadFrom(src).
func Copy(dst Writer, src Reader) (written int64, err error) {
	return copyBuffer(dst, src, nil)
}

// CopyBuffer is identical to Copy except that it stages through the
// provided buffer (if one is required) rather than allocating a
// temporary one. If buf is nil, one is allocated; otherwise if it has
// zero length, CopyBuffer panics.
//
// If either src implements [WriterTo] or dst implements [ReaderFrom],
// buf will not be used to perform the copy.
func CopyBuffer(dst Writer, src Reader, buf []byte) (written int64, err error) {
	if buf != nil && len(buf) == 0 {
		panic("empty buffer in CopyBuffer")
	}
	return copyBuffer(dst, src, buf)
}

// copyBuffer is the actual implementation of Copy and CopyBuffer.
// if buf is nil, one is allocated.
func copyBuffer(dst Writer, src Reader, buf []byte) (written int64, err error) {
	// If the reader has a WriteTo method, use it to do the copy.
	// Avoids an allocation and a copy.
	if wt, ok := src.(WriterTo); ok {
		return wt.WriteTo(dst)
	}
	// Similarly, if the writer has a ReadFrom method, use it to do the copy.
	if rf, ok := dst.(ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	if buf == nil {
		size := 32 * 1024
		if l, ok := src.(*LimitedReader); ok && int64(size) > l.N {
			if l.N < 1 {
				size = 1
			} else {
				size = int(l.N)
			}
		}
		buf = make([]byte, size)
	}
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = errInvalidWrite
				}
			}
			written += int64(nw)
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != EOF {
				err = er
			}
			break
		}
	}
	return written, err
}

// LimitReader returns a Reader that reads from r
// but stops with EOF after n bytes.
// The underlying implementation is a *LimitedReader.
func LimitReader(r Reader, n int64) Reader { return &LimitedReader{r, n} }

// A LimitedReader reads from R but limits the amount of
// data returned to just N bytes. Each call to Read
// updates N to reflect the new amount remaining.
// Read returns EOF when N <= 0 or when the underlying R returns EOF.
type LimitedReader struct {
	R Reader // underlying reader
	N int64  // max bytes remaining
}

func (l *LimitedReader) Read(p []byte) (n int, err error) {
	if l.N <= 0 {
		return 0, EOF
	}
	if int64(len(p)) > l.N {
		p = p[0:l.N]
	}
	n, err = l.R.Read(p)
	l.N -= int64(n)
	return
}

// NewSectionReader returns a [SectionReader] that reads from r
// starting at offset off and stops with EOF after n bytes.
func NewSectionReader(r ReaderAt, off int64, n int64) *SectionReader {
	var remaining int64
	const maxint64 = 1<<63 - 1
	if off <= maxint64-n {
		remaining = n + off
	} else {
		// Overflow, with no way to return error.
		// Assume we can read up to an offset of 1<<63 - 1.
		remaining = maxint64
	}
	return &SectionReader{r, off, off, remaining, n}
}

// SectionReader implements Read, Seek, and ReadAt on a section
// of an underlying [ReaderAt].
type SectionReader struct {
	r     ReaderAt // constant after creation
	base  int64    // constant after creation
	off   int64
	limit int64 // constant after creation
	n     int64 // constant after creation
}

func (s *SectionReader) Read(p []byte) (n int, err error) {
	if s.off >= s.limit {
		return 0, EOF
	}
	if max := s.limit - s.off; int64(len(p)) > max {
		p = p[0:max]
	}
	n, err = s.r.ReadAt(p, s.off)
	s.off += int64(n)
	return
}

var errWhence = errors.New("Seek: invalid whence")
var errOffset = errors.New("Seek: invalid offset")

func (s *SectionReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	default:
		return 0, errWhence
	case SeekStart:
		offset += s.base
	case SeekCurrent:
		offset += s.off
	case SeekEnd:
		offset += s.limit
	}
	if offset < s.base {
		return 0, errOffset
	}
	s.off = offset
	return offset - s.base, nil
}

func (s *SectionReader) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 || off >= s.Size() {
		return 0, EOF
	}
	off += s.base
	if max := s.limit - off; int64(len(p)) > max {
		p = p[0:max]
		n, err = s.r.ReadAt(p, off)
		if err == nil {
			err = EOF
		}
		return n, err
	}
	return s.r.ReadAt(p, off)
}

// Size returns the size of the section in bytes.
func (s *SectionReader) Size() int64 { return s.limit - s.base }

// Outer returns the underlying [ReaderAt] and offsets for the section.
//
// The returned values are the same that were passed to [NewSectionReader]
// when the [SectionReader] was created.
func (s *SectionReader) Outer() (r ReaderAt, off int64, n int64) {
	return s.r, s.base, s.n
}

// An OffsetWriter maps writes at offset base to offset base+off in the underlying writer.
type OffsetWriter struct {
	w    WriterAt
	base int64 // the original offset
	off  int64 // the current offset
}

// NewOffsetWriter returns an [OffsetWriter] that writes to w
// starting at offset off.
func NewOffsetWriter(w WriterAt, off int64) *OffsetWriter {
	return &OffsetWriter{w, off, off}
}

func (o *OffsetWriter) Write(p []byte) (n int, err error) {
	n, err = o.w.WriteAt(p, o.off)
	o.off += int64(n)
	return
}

func (o *OffsetWriter) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, errOffset
	}

	off += o.base
	return o.w.WriteAt(p, off)
}

func (o *OffsetWriter) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	default:
		return 0, errWhence
	case SeekStart:
		offset += o.base
	case SeekCurrent:
		offset += o.off
	}
	if offset < o.base {
		return 0, errOffset
	}
	o.off = offset
	return offset - o.base, nil
}

// TeeReader returns a [Reader] that writes to w what it reads from r.
// All reads from r performed through it are matched with
// corresponding writes to w. There is no internal buffering -
// the write must complete before the read completes.
// Any error encountered while writing is reported as a read error.
func TeeReader(r Reader, w Writer) Reader {
	return &teeReader{r, w}
}

type teeReader struct {
	r Reader
	w Writer
}

func (t *teeReader) Read(p []byte) (n int, err error) {
	n, err = t.r.Read(p)
	if n > 0 {
		if n, err := t.w.Write(p[:n]); err != nil {
			return n, err
		}
	}
	return
}

// Discard is a [Writer] on which all Write calls succeed
// without doing anything.
var Discard Writer = discard{}

type discard struct{}

// discard implements ReaderFrom as an optimization so Copy to
// io.Discard can avoid doing unnecessary work.
var _ ReaderFrom = discard{}

func (discard) Write(p []byte) (int, error) {
	return len(p), nil
}

func (discard) WriteString(s string) (int, error) {
	return len(s), nil
}

var blackHolePool = sync.Pool{
	New: func() any {
		b := make([]byte, 8192)
		return &b
	},
}

func (discard) ReadFrom(r Reader) (n int64, err error) {
	bufp := blackHolePool.Get().(*[]byte)
	readSize := 0
	for {
		readSize, err = r.Read(*bufp)
		n += int64(readSize)
		if err != nil {
			blackHolePool.Put(bufp)
			if err == EOF {
				return n, nil
			}
			return
		}
	}
}

// NopCloser returns a [ReadCloser] with a no-op Close method wrapping
// the provided [Reader] r.
// If r implements [WriterTo], the returned [ReadCloser] will implement [WriterTo]
// by forwarding calls to r.
func NopCloser(r Reader) ReadCloser {
	if _, ok := r.(WriterTo); ok {
		return nopCloserWriterTo{r}
	}
	return nopCloser{r}
}

type nopCloser struct {
	Reader
}

func (nopCloser) Close() error { return nil }

type nopCloserWriterTo struct {
	Reader
}

func (nopCloserWriterTo) Close() error { return nil }

func (c nopCloserWriterTo) WriteTo(w Writer) (n int64, err error) {
	return c.Reader.(WriterTo).WriteTo(w)
}

// ReadAll reads from r until an error or EOF and returns the data it read.
// A successful call returns err == nil, not err == EOF. Because ReadAll is
// defined to read from src until EOF, it does not treat an EOF from Read
// as an error to be reported.
func ReadAll(r Reader) ([]byte, error) {
	// Build slices of exponentially growing size,
	// then copy into a perfectly-sized slice at the end.
	b := make([]byte, 0, 512)
	// Starting with next equal to 256 (instead of say 512 or 1024)
	// allows less memory usage for small inputs that finish in the
	// early growth stages, but we grow the read sizes quickly such that
	// it does not materially impact medium or large inputs.
	next := 256
	chunks := make([][]byte, 0, 4)
	// Invariant: finalSize = sum(len(c) for c in chunks)
	var finalSize int
	for {
		n, err := r.Read(b[len(b):cap(b)])
		b = b[:len(b)+n]
		if err != nil {
			if err == EOF {
				err = nil
			}
			if len(chunks) == 0 {
				return b, err
			}

			// Build our final right-sized slice.
			finalSize += len(b)
			final := append([]byte(nil), make([]byte, finalSize)...)[:0]
			for _, chunk := range chunks {
				final = append(final, chunk...)
			}
			final = append(final, b...)
			return final, err
		}

		if cap(b)-len(b) < cap(b)/16 {
			// Move to the next intermediate slice.
			chunks = append(chunks, b)
			finalSize += len(b)
			b = append([]byte(nil), make([]byte, next)...)[:0]
			next += next / 2
		}
	}
}
