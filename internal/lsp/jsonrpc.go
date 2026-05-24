package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/Ondotteess/kleiber/internal/logging"
)

// jsonRPCVersion is the only protocol version this codec accepts.
// Both peers MUST emit "jsonrpc": "2.0"; anything else is a wire error.
const jsonRPCVersion = "2.0"

// DefaultMaxPayloadBytes caps the size of one JSON-RPC body, in either
// direction. LSP servers occasionally emit large publishDiagnostics
// frames; 16 MiB is well above what gopls produces in practice and well
// below what would put memory pressure on the editor.
const DefaultMaxPayloadBytes = 16 * 1024 * 1024

// maxHeaderLineBytes bounds a single LSP header line. The LSP base
// protocol does not specify a maximum; this is a defensive limit aimed
// at malformed framing rather than at gopls, which emits ~30-byte
// header lines.
const maxHeaderLineBytes = 1024

// maxHeaderLines bounds how many header lines we will read before
// declaring the frame malformed.
const maxHeaderLines = 32

// JSON-RPC 2.0 error codes (jsonrpc.org/specification §5.1). LSP extends
// this range with -32099..-32000 (server-reserved); call sites that need
// LSP-specific codes can define them locally.
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
)

// Sentinel errors. Callers compare with errors.Is.
var (
	// ErrBadVersion indicates the peer sent a "jsonrpc" field other
	// than "2.0".
	ErrBadVersion = errors.New("jsonrpc: missing or unsupported version")

	// ErrBadFraming indicates malformed LSP base-protocol framing —
	// missing CRLF, garbled header, or impossible Content-Length.
	ErrBadFraming = errors.New("jsonrpc: bad frame")

	// ErrMissingContentLength indicates the frame header block ended
	// without a Content-Length line.
	ErrMissingContentLength = errors.New("jsonrpc: missing Content-Length header")

	// ErrPayloadTooLarge indicates a frame body exceeds the configured
	// maximum payload size.
	ErrPayloadTooLarge = errors.New("jsonrpc: payload exceeds maximum size")

	// ErrMalformedMessage indicates the JSON parsed but does not fit
	// any of Request, Notification, Response.
	ErrMalformedMessage = errors.New("jsonrpc: malformed message")

	// ErrHeaderTooLong indicates a single header line exceeded
	// maxHeaderLineBytes.
	ErrHeaderTooLong = errors.New("jsonrpc: header line too long")

	// ErrTooManyHeaders indicates the header block had more lines than
	// maxHeaderLines before reaching the empty terminator.
	ErrTooManyHeaders = errors.New("jsonrpc: too many header lines")

	// ErrEmptyResponse indicates an attempt to marshal a Response that
	// has neither Result nor Error set.
	ErrEmptyResponse = errors.New("jsonrpc: response has neither result nor error")

	// ErrAmbiguousResponse indicates an attempt to marshal a Response
	// that has both Result and Error set.
	ErrAmbiguousResponse = errors.New("jsonrpc: response has both result and error")
)

// ID is a JSON-RPC message identifier. It is either an integer or a
// string. The spec also permits null, which we accept on decode (used by
// servers for error responses to unparseable requests) but never emit.
//
// The zero value is the numeric ID 0; construct explicit IDs with
// NewIntID or NewStringID.
type ID struct {
	isString bool
	isNull   bool
	str      string
	num      int64
}

// NewIntID constructs an integer ID.
func NewIntID(n int64) ID { return ID{num: n} }

// NewStringID constructs a string ID.
func NewStringID(s string) ID { return ID{isString: true, str: s} }

// Int64 reports the ID as a 64-bit integer. The boolean is false if the
// ID is a string or null.
func (id ID) Int64() (int64, bool) {
	if id.isString || id.isNull {
		return 0, false
	}
	return id.num, true
}

// AsString reports the ID as a string. The boolean is false if the ID is
// an integer or null.
func (id ID) AsString() (string, bool) {
	if !id.isString || id.isNull {
		return "", false
	}
	return id.str, true
}

// IsNull reports whether the ID was the JSON literal null.
func (id ID) IsNull() bool { return id.isNull }

// MarshalJSON encodes the ID as either an integer or a string. A null ID
// (from decoding) is emitted as JSON null.
func (id ID) MarshalJSON() ([]byte, error) {
	switch {
	case id.isNull:
		return []byte("null"), nil
	case id.isString:
		return json.Marshal(id.str)
	default:
		return json.Marshal(id.num)
	}
}

// UnmarshalJSON decodes the ID. It accepts integers, strings, and the
// JSON literal null. Floats, arrays, and objects are rejected.
func (id *ID) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("jsonrpc: empty ID payload")
	}
	if string(data) == "null" {
		id.isNull = true
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("jsonrpc: parsing ID as string: %w", err)
		}
		id.isString = true
		id.str = s
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("jsonrpc: parsing ID as integer: %w", err)
	}
	id.num = n
	return nil
}

// Message is a JSON-RPC 2.0 message: Request, Notification, or
// Response. The set is closed (sealed by isMessage); switch on the
// concrete pointer type to dispatch.
type Message interface {
	isMessage()
}

// Request is a JSON-RPC request expecting a Response with the same ID.
type Request struct {
	// ID is the unique correlation identifier.
	ID ID

	// Method is the dotted method name (e.g., "textDocument/hover").
	Method string

	// Params is the raw JSON of the parameters object. The codec does
	// not decode it; the caller knows the schema based on Method.
	Params json.RawMessage
}

func (*Request) isMessage() {}

// MarshalJSON emits the request in canonical JSON-RPC 2.0 form.
func (r *Request) MarshalJSON() ([]byte, error) {
	w := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      ID              `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}{
		JSONRPC: jsonRPCVersion,
		ID:      r.ID,
		Method:  r.Method,
		Params:  r.Params,
	}
	return json.Marshal(w)
}

// Notification is a JSON-RPC notification: no response expected, no ID.
type Notification struct {
	// Method is the dotted method name.
	Method string

	// Params is the raw JSON of the parameters object.
	Params json.RawMessage
}

func (*Notification) isMessage() {}

// MarshalJSON emits the notification in canonical JSON-RPC 2.0 form.
func (n *Notification) MarshalJSON() ([]byte, error) {
	w := struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}{
		JSONRPC: jsonRPCVersion,
		Method:  n.Method,
		Params:  n.Params,
	}
	return json.Marshal(w)
}

// Response is a JSON-RPC response. Exactly one of Result or Error must
// be set; MarshalJSON enforces this.
type Response struct {
	// ID matches the request ID. May be null when the server failed
	// to parse the request.
	ID ID

	// Result is the raw JSON of the result. nil means "use Error".
	Result json.RawMessage

	// Error, when non-nil, carries the failure reason.
	Error *ResponseError
}

func (*Response) isMessage() {}

// MarshalJSON emits the response in canonical JSON-RPC 2.0 form.
//
// It returns ErrEmptyResponse if both Result and Error are unset, and
// ErrAmbiguousResponse if both are set.
func (r *Response) MarshalJSON() ([]byte, error) {
	hasResult := r.Result != nil
	hasError := r.Error != nil
	switch {
	case !hasResult && !hasError:
		return nil, ErrEmptyResponse
	case hasResult && hasError:
		return nil, ErrAmbiguousResponse
	}
	w := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      ID              `json:"id"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *ResponseError  `json:"error,omitempty"`
	}{
		JSONRPC: jsonRPCVersion,
		ID:      r.ID,
		Result:  r.Result,
		Error:   r.Error,
	}
	return json.Marshal(w)
}

// ResponseError carries the failure of a Response.
type ResponseError struct {
	// Code is the JSON-RPC or LSP error code. See ErrCode* constants.
	Code int `json:"code"`

	// Message is a short human-readable description.
	Message string `json:"message"`

	// Data is optional structured detail.
	Data json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface so a ResponseError can flow
// through error-handling chains.
func (e *ResponseError) Error() string {
	if e == nil {
		return "<nil jsonrpc error>"
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// Conn is a JSON-RPC 2.0 connection framed by the LSP base protocol
// (Content-Length header, CRLF separator, UTF-8 JSON body).
//
// Read must be called from a single goroutine. Write is safe for
// concurrent use: an internal mutex serializes the header+body pair so
// concurrent writers cannot interleave frames.
//
// Conn does not own the underlying reader/writer streams; close them on
// the caller side (typically by stopping the subprocess that supplies
// stdio).
type Conn struct {
	logger *slog.Logger

	maxPayload int

	r *bufio.Reader

	mu sync.Mutex
	w  io.Writer
}

// ConnOptions configures NewConn.
type ConnOptions struct {
	// Reader is the byte source — e.g., a subprocess's stdout.
	Reader io.Reader

	// Writer is the byte sink — e.g., a subprocess's stdin.
	Writer io.Writer

	// Logger receives debug-level diagnostics. Nil means discard.
	Logger *slog.Logger

	// MaxPayloadBytes overrides DefaultMaxPayloadBytes when positive.
	MaxPayloadBytes int
}

// NewConn constructs a Conn over the supplied reader and writer.
//
// The reader is wrapped in a bufio.Reader internally; do not wrap it
// again before passing in — buffer-on-buffer leads to lost bytes when
// the inner buffer drains independently.
func NewConn(opts ConnOptions) *Conn {
	logger := opts.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	max := opts.MaxPayloadBytes
	if max <= 0 {
		max = DefaultMaxPayloadBytes
	}
	return &Conn{
		logger:     logger,
		maxPayload: max,
		r:          bufio.NewReader(opts.Reader),
		w:          opts.Writer,
	}
}

// Read decodes the next message from the underlying reader. It blocks
// until a complete frame is available or the underlying reader returns
// an error (typically io.EOF on subprocess exit).
//
// Read is not safe for concurrent use; call it from a single goroutine.
func (c *Conn) Read() (Message, error) {
	contentLength, err := c.readHeaders()
	if err != nil {
		return nil, err
	}
	if contentLength > c.maxPayload {
		return nil, fmt.Errorf("%w: declared %d, max %d",
			ErrPayloadTooLarge, contentLength, c.maxPayload)
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.r, body); err != nil {
		// Once headers committed us to a body of N bytes, any EOF on
		// the body is unexpected — even a "clean" EOF with 0 bytes
		// read. Normalize so callers can match io.ErrUnexpectedEOF.
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("jsonrpc: reading body: %w", err)
	}
	msg, err := decodeMessage(body)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// Write encodes msg and emits it as a single LSP frame. It is safe for
// concurrent use.
//
// Write does not retry on partial writes; the underlying writer is
// expected to behave like a kernel pipe (atomic for buffer-sized
// writes) or be wrapped by the caller.
func (c *Conn) Write(msg Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("jsonrpc: marshaling message: %w", err)
	}
	if len(body) > c.maxPayload {
		return fmt.Errorf("%w: marshaled %d, max %d",
			ErrPayloadTooLarge, len(body), c.maxPayload)
	}

	// Build header+body in a single buffer so a single Write syscall
	// keeps the frame atomic with respect to other writers holding the
	// mutex. The 24-byte slack covers "Content-Length: " plus a long
	// integer plus two CRLFs.
	buf := make([]byte, 0, len(body)+24+len(strconv.Itoa(len(body))))
	buf = append(buf, "Content-Length: "...)
	buf = strconv.AppendInt(buf, int64(len(body)), 10)
	buf = append(buf, '\r', '\n', '\r', '\n')
	buf = append(buf, body...)

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.w.Write(buf); err != nil {
		return fmt.Errorf("jsonrpc: writing frame: %w", err)
	}
	return nil
}

// readHeaders consumes the LSP frame header block and returns the
// declared Content-Length. It returns ErrMissingContentLength if the
// block ends without one, and ErrBadFraming for malformed lines.
func (c *Conn) readHeaders() (int, error) {
	contentLength := -1
	for i := 0; i < maxHeaderLines; i++ {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return 0, fmt.Errorf("jsonrpc: reading header: %w", err)
		}
		if len(line) > maxHeaderLineBytes {
			return 0, fmt.Errorf("%w: %d bytes", ErrHeaderTooLong, len(line))
		}
		if !strings.HasSuffix(line, "\r\n") {
			return 0, fmt.Errorf("%w: header line missing CRLF", ErrBadFraming)
		}
		line = strings.TrimSuffix(line, "\r\n")
		if line == "" {
			if contentLength < 0 {
				return 0, ErrMissingContentLength
			}
			return contentLength, nil
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return 0, fmt.Errorf("%w: header %q missing colon", ErrBadFraming, line)
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if strings.EqualFold(name, "Content-Length") {
			n, err := strconv.Atoi(value)
			if err != nil {
				return 0, fmt.Errorf("%w: Content-Length %q: %w", ErrBadFraming, value, err)
			}
			if n < 0 {
				return 0, fmt.Errorf("%w: negative Content-Length %d", ErrBadFraming, n)
			}
			contentLength = n
		}
		// Any other header (notably Content-Type) is ignored per LSP
		// base protocol §"Header Part".
	}
	return 0, ErrTooManyHeaders
}

// wireMessage is the union of all three message shapes used purely for
// decoding. Marshaling goes through the typed MarshalJSON above.
type wireMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *ID             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

// decodeMessage interprets a JSON body as one of the three message
// types. It is exposed unexported so tests in the same package can
// invoke it directly without setting up a full Conn.
func decodeMessage(body []byte) (Message, error) {
	var w wireMessage
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedMessage, err)
	}
	if w.JSONRPC != jsonRPCVersion {
		return nil, fmt.Errorf("%w: got %q", ErrBadVersion, w.JSONRPC)
	}

	hasID := w.ID != nil
	hasMethod := w.Method != ""
	hasResult := w.Result != nil
	hasError := w.Error != nil

	switch {
	case hasMethod && hasID:
		return &Request{ID: *w.ID, Method: w.Method, Params: w.Params}, nil
	case hasMethod && !hasID:
		return &Notification{Method: w.Method, Params: w.Params}, nil
	case !hasMethod && hasID:
		if hasResult == hasError {
			return nil, fmt.Errorf("%w: response must have exactly one of result/error", ErrMalformedMessage)
		}
		return &Response{ID: *w.ID, Result: w.Result, Error: w.Error}, nil
	default:
		return nil, fmt.Errorf("%w: needs method (request/notification) or id with result/error (response)", ErrMalformedMessage)
	}
}
