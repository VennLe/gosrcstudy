// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate bundle -o=h2_bundle.go -prefix=http2 -tags=!nethttpomithttp2 -import=golang.org/x/net/internal/httpcommon=net/http/internal/httpcommon golang.org/x/net/http2

package http

import (
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/http/httpguts"
)

// Protocols 是一组 HTTP 协议。
// 零值是一个空的协议集合。
//
// 支持的协议包括：
//
//   - HTTP1: HTTP/1.0 和 HTTP/1.1 协议。
//     HTTP1 既支持未加密的 TCP 连接，也支持加密的 TLS 连接。
//
//   - HTTP2: 基于 TLS 连接的 HTTP/2 协议。
//
//   - UnencryptedHTTP2: 基于未加密 TCP 连接的 HTTP/2 协议。
type Protocols struct {
	bits uint8
}

const (
	protoHTTP1 = 1 << iota
	protoHTTP2
	protoUnencryptedHTTP2
)

// HTTP1 报告 p 是否包含对 HTTP/1 协议的支持。
func (p Protocols) HTTP1() bool { return p.bits&protoHTTP1 != 0 }

// SetHTTP1 用于在 p 中添加或移除对 HTTP/1 协议的支持。
func (p *Protocols) SetHTTP1(ok bool) { p.setBit(protoHTTP1, ok) }

// HTTP2 报告 p 是否包含对 HTTP/2 协议的支持。
func (p Protocols) HTTP2() bool { return p.bits&protoHTTP2 != 0 }

// SetHTTP2 用于在 p 中添加或移除对 HTTP/2 协议的支持。
func (p *Protocols) SetHTTP2(ok bool) { p.setBit(protoHTTP2, ok) }

// UnencryptedHTTP2 报告 p 是否包含对未加密 HTTP/2 协议的支持。
func (p Protocols) UnencryptedHTTP2() bool { return p.bits&protoUnencryptedHTTP2 != 0 }

// SetUnencryptedHTTP2 用于在 p 中添加或移除对未加密 HTTP/2 协议的支持。
func (p *Protocols) SetUnencryptedHTTP2(ok bool) { p.setBit(protoUnencryptedHTTP2, ok) }

func (p *Protocols) setBit(bit uint8, ok bool) {
	if ok {
		p.bits |= bit
	} else {
		p.bits &^= bit
	}
}

func (p Protocols) String() string {
	var s []string
	if p.HTTP1() {
		s = append(s, "HTTP1")
	}
	if p.HTTP2() {
		s = append(s, "HTTP2")
	}
	if p.UnencryptedHTTP2() {
		s = append(s, "UnencryptedHTTP2")
	}
	return "{" + strings.Join(s, ",") + "}"
}

// incomparable 是一个零宽度、不可比较的类型。将其添加到结构体中，
// 会使该结构体也变得不可比较，并且通常不会增加额外的大小（只要将它放在首位）。
type incomparable [0]func()

// maxInt64 是 Server 和 Transport 中字节限制读取器实际使用的“无限”值。
const maxInt64 = 1<<63 - 1

// aLongTimeAgo 是一个非零的过去时间点，用于立即取消网络操作。
var aLongTimeAgo = time.Unix(1, 0)

// omitBundledHTTP2 在设置 nethttpomithttp2 构建标签时，
// 由 omithttp2.go 文件设置。这意味着 h2_bundle.go 未被编译，
// 我们不应尝试使用它。
var omitBundledHTTP2 bool

// TODO(bradfitz)：将公共内容移至此处。其他文件中在随机位置累积了通用的 HTTP 相关代码。

// contextKey 用于与 context.WithValue 配合使用的键类型。
// 它被定义为指针类型，从而可以在不分配内存的情况下存入 interface{}。
type contextKey struct {
	name string
}

func (k *contextKey) String() string { return "net/http context value " + k.name }

// 给定格式为 "host"、"host:port" 或 "[ipv6::address]:port" 的字符串，
// 如果字符串包含端口号，则返回 true。
func hasPort(s string) bool { return strings.LastIndex(s, ":") > strings.LastIndex(s, "]") }

// removeEmptyPort 按照 RFC 3986 第 6.2.3 节的要求，将 ":port" 中的空端口转换为 ""。
func removeEmptyPort(host string) string {
	if hasPort(host) {
		return strings.TrimSuffix(host, ":")
	}
	return host
}

// isToken 检查 v 是否是一个有效的 token(https://www.rfc-editor.org/rfc/rfc2616#section-2.2).
func isToken(v string) bool {
	// 由于历史原因，此函数被命名为 ValidHeaderFieldName（参见 issue #67031）。
	return httpguts.ValidHeaderFieldName(v)
}

// stringContainsCTLByte 检查字符串是否包含任何 ASCII 控制字符。
func stringContainsCTLByte(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < ' ' || b == 0x7f {
			return true
		}
	}
	return false
}

func hexEscapeNonASCII(s string) string {
	newLen := 0
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			newLen += 3
		} else {
			newLen++
		}
	}
	if newLen == len(s) {
		return s
	}
	b := make([]byte, 0, newLen)
	var pos int
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			if pos < i {
				b = append(b, s[pos:i]...)
			}
			b = append(b, '%')
			b = strconv.AppendInt(b, int64(s[i]), 16)
			pos = i + 1
		}
	}
	if pos < len(s) {
		b = append(b, s[pos:]...)
	}
	return string(b)
}

// NoBody 是一个无内容的 [io.ReadCloser]。读取时始终返回 EOF，
// 关闭时始终返回 nil。可在发起客户端请求时使用，以明确表示请求体无内容。
// 另一种做法是直接将 [Request.Body] 设置为 nil。
var NoBody = noBody{}

type noBody struct{}

func (noBody) Read([]byte) (int, error)         { return 0, io.EOF }
func (noBody) Close() error                     { return nil }
func (noBody) WriteTo(io.Writer) (int64, error) { return 0, nil }

var (
	// 验证从 NoBody 执行 io.Copy 操作不会需要缓冲区：
	_ io.WriterTo   = NoBody
	_ io.ReadCloser = NoBody
)

// PushOptions 描述了 [Pusher.Push] 方法的选项。
type PushOptions struct {
	// Method 指定了所承诺请求的 HTTP 方法。
	// 如果设置了此值，它必须为 "GET" 或 "HEAD"。为空时默认为 "GET"。
	Method string

	// Header 指定了所承诺请求的额外头部字段。此字段不能包含 HTTP/2 伪头部字段（例如 ":path" 和 ":scheme"），这些伪头部字段将由系统自动添加。
	Header Header
}

// Pusher 是由支持 HTTP/2 服务器推送的 ResponseWriters 实现的接口。更多背景信息，请参考 。
type Pusher interface {
	// Push 方法用于发起一个 HTTP/2 服务器推送。它会根据给定的目标路径/URL 和选项构造一个合成请求，
	// 将该请求序列化为 PUSH_PROMISE 帧，然后通过服务器的请求处理器分发该请求。如果 opts 为 nil，
	// 将使用默认选项。
	//
	// 目标（target）必须是绝对路径（例如 "/path"），或者是包含有效主机名且与父请求协议方案相同的绝对 URL。
	// 如果目标是路径，它将继承父请求的协议方案和主机名。
	//
	// HTTP/2 规范禁止递归推送和跨域推送。本方法可能不会检测到这些无效推送；
	// 然而，符合规范的客户端会检测并取消这些无效推送。
	//
	// 希望推送 URL X 的处理器应在发送任何可能触发对 URL X 的请求的数据之前调用 Push 方法。
	// 这可以避免客户端在收到针对 X 的 PUSH_PROMISE 帧之前就发起对 X 的请求的竞态条件。
	//
	// Push 方法会在单独的 goroutine 中运行，因此推送到达的顺序是非确定性的。
	// 任何必要的同步都需要由调用方实现。
	//
	// 如果客户端禁用了推送功能，或者底层连接不支持推送，Push 方法会返回 ErrNotSupported 错误。
	Push(target string, opts *PushOptions) error
}

// HTTP2Config 定义了 HTTP/2 配置参数，这些参数在 [Transport] 和 [Server] 中是通用的。
type HTTP2Config struct {
	// MaxConcurrentStreams 可选地指定客户端在同一时间可打开的最大并发流数。
	// 如果值为零，则 MaxConcurrentStreams 默认至少为 100。
	//
	// 此参数仅适用于服务器端。
	MaxConcurrentStreams int

	// StrictMaxConcurrentRequests 控制是否应在与某个 HTTP/2 服务器的所有连接上强制执行其并发限制。
	// 如果为 true，当某个连接达到其并发限制时，发送的新请求将会阻塞，直到已有请求完成。
	// 如果为 false，如果所有现有连接都达到了各自的限制，将会打开一个新的连接来处理新请求。
	//
	// 此参数仅适用于传输层（Transports）。
	StrictMaxConcurrentRequests bool

	// MaxDecoderHeaderTableSize optionally specifies an upper limit for the
	// size of the header compression table used for decoding headers sent
	// by the peer.
	// A valid value is less than 4MiB.
	// If zero or invalid, a default value is used.
	MaxDecoderHeaderTableSize int

	// MaxEncoderHeaderTableSize optionally specifies an upper limit for the
	// header compression table used for sending headers to the peer.
	// A valid value is less than 4MiB.
	// If zero or invalid, a default value is used.
	MaxEncoderHeaderTableSize int

	// MaxReadFrameSize optionally specifies the largest frame
	// this endpoint is willing to read.
	// A valid value is between 16KiB and 16MiB, inclusive.
	// If zero or invalid, a default value is used.
	MaxReadFrameSize int

	// MaxReceiveBufferPerConnection is the maximum size of the
	// flow control window for data received on a connection.
	// A valid value is at least 64KiB and less than 4MiB.
	// If invalid, a default value is used.
	MaxReceiveBufferPerConnection int

	// MaxReceiveBufferPerStream is the maximum size of
	// the flow control window for data received on a stream (request).
	// A valid value is less than 4MiB.
	// If zero or invalid, a default value is used.
	MaxReceiveBufferPerStream int

	// SendPingTimeout is the timeout after which a health check using a ping
	// frame will be carried out if no frame is received on a connection.
	// If zero, no health check is performed.
	SendPingTimeout time.Duration

	// PingTimeout is the timeout after which a connection will be closed
	// if a response to a ping is not received.
	// If zero, a default of 15 seconds is used.
	PingTimeout time.Duration

	// WriteByteTimeout is the timeout after which a connection will be
	// closed if no data can be written to it. The timeout begins when data is
	// available to write, and is extended whenever any bytes are written.
	WriteByteTimeout time.Duration

	// PermitProhibitedCipherSuites, if true, permits the use of
	// cipher suites prohibited by the HTTP/2 spec.
	PermitProhibitedCipherSuites bool

	// CountError, if non-nil, is called on HTTP/2 errors.
	// It is intended to increment a metric for monitoring.
	// The errType contains only lowercase letters, digits, and underscores
	// (a-z, 0-9, _).
	CountError func(errType string)
}
