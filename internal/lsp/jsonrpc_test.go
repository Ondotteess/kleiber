package lsp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

// roundTrip writes msg through a Conn into an in-memory buffer, then
// reads it back through a fresh Conn over the same bytes. This avoids
// the deadlock potential of io.Pipe-based duplex tests, where a failing
// Read leaves a Write goroutine blocked indefinitely.
func roundTrip(t *testing.T, msg Message) Message {
	t.Helper()
	var buf bytes.Buffer
	if err := NewConn(ConnOptions{Writer: &buf}).Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := NewConn(ConnOptions{Reader: &buf}).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return got
}

func TestConn_RoundTrip_Request(t *testing.T) {
	want := &Request{
		ID:     NewIntID(7),
		Method: "textDocument/hover",
		Params: json.RawMessage(`{"textDocument":{"uri":"file:///x.go"},"position":{"line":1,"character":2}}`),
	}
	req, ok := roundTrip(t, want).(*Request)
	if !ok {
		t.Fatal("roundTrip did not return *Request")
	}
	if n, _ := req.ID.Int64(); n != 7 {
		t.Errorf("ID.Int64 = %d, want 7", n)
	}
	if req.Method != want.Method {
		t.Errorf("Method = %q, want %q", req.Method, want.Method)
	}
	if string(req.Params) != string(want.Params) {
		t.Errorf("Params = %s, want %s", req.Params, want.Params)
	}
}

func TestConn_RoundTrip_Notification(t *testing.T) {
	want := &Notification{
		Method: "textDocument/didChange",
		Params: json.RawMessage(`{"uri":"file:///x.go","contentChanges":[]}`),
	}
	n, ok := roundTrip(t, want).(*Notification)
	if !ok {
		t.Fatal("roundTrip did not return *Notification")
	}
	if n.Method != want.Method {
		t.Errorf("Method = %q, want %q", n.Method, want.Method)
	}
}

func TestConn_RoundTrip_ResponseSuccess(t *testing.T) {
	want := &Response{
		ID:     NewIntID(42),
		Result: json.RawMessage(`{"contents":"hover text"}`),
	}
	r, ok := roundTrip(t, want).(*Response)
	if !ok {
		t.Fatal("roundTrip did not return *Response")
	}
	if n, _ := r.ID.Int64(); n != 42 {
		t.Errorf("ID.Int64 = %d, want 42", n)
	}
	if r.Error != nil {
		t.Errorf("Error = %v, want nil", r.Error)
	}
	if string(r.Result) != string(want.Result) {
		t.Errorf("Result = %s, want %s", r.Result, want.Result)
	}
}

func TestConn_RoundTrip_ResponseError(t *testing.T) {
	want := &Response{
		ID: NewIntID(99),
		Error: &ResponseError{
			Code:    ErrCodeMethodNotFound,
			Message: "method not found",
			Data:    json.RawMessage(`{"method":"foo/bar"}`),
		},
	}
	r, ok := roundTrip(t, want).(*Response)
	if !ok {
		t.Fatal("roundTrip did not return *Response")
	}
	if r.Result != nil {
		t.Errorf("Result = %s, want nil", r.Result)
	}
	if r.Error == nil {
		t.Fatal("Error is nil, want non-nil")
	}
	if r.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("Error.Code = %d, want %d", r.Error.Code, ErrCodeMethodNotFound)
	}
	// ResponseError satisfies error.
	var asErr error = r.Error
	if !strings.Contains(asErr.Error(), "method not found") {
		t.Errorf("asErr = %q, want it to mention method not found", asErr.Error())
	}
}

func TestConn_Write_RejectsEmptyResponse(t *testing.T) {
	var buf bytes.Buffer
	c := NewConn(ConnOptions{Writer: &buf})
	err := c.Write(&Response{ID: NewIntID(1)})
	if !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("err = %v, want ErrEmptyResponse", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes for empty response, want 0", buf.Len())
	}
}

func TestConn_Write_RejectsAmbiguousResponse(t *testing.T) {
	var buf bytes.Buffer
	c := NewConn(ConnOptions{Writer: &buf})
	err := c.Write(&Response{
		ID:     NewIntID(1),
		Result: json.RawMessage(`"ok"`),
		Error:  &ResponseError{Code: 1, Message: "x"},
	})
	if !errors.Is(err, ErrAmbiguousResponse) {
		t.Errorf("err = %v, want ErrAmbiguousResponse", err)
	}
}

func TestConn_Decode_TolerantContentType(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	frame := fmt.Sprintf(
		"Content-Length: %d\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n%s",
		len(body), body,
	)
	c := NewConn(ConnOptions{Reader: strings.NewReader(frame)})
	got, err := c.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, ok := got.(*Request); !ok {
		t.Fatalf("got %T, want *Request", got)
	}
}

func TestConn_Decode_HeaderOrderIndependent(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	frame := fmt.Sprintf(
		"Content-Type: application/vscode-jsonrpc; charset=utf-8\r\nContent-Length: %d\r\n\r\n%s",
		len(body), body,
	)
	c := NewConn(ConnOptions{Reader: strings.NewReader(frame)})
	if _, err := c.Read(); err != nil {
		t.Errorf("Read: %v", err)
	}
}

func TestConn_Decode_BadContentLength(t *testing.T) {
	cases := []struct {
		name  string
		frame string
	}{
		{"non-numeric", "Content-Length: abc\r\n\r\n{}"},
		{"negative", "Content-Length: -5\r\n\r\n{}"},
		{"missing", "Content-Type: text/plain\r\n\r\n{}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewConn(ConnOptions{Reader: strings.NewReader(tc.frame)})
			_, err := c.Read()
			if err == nil {
				t.Fatal("Read: nil error, want some framing error")
			}
		})
	}
}

func TestConn_Decode_BodyTooLarge(t *testing.T) {
	// Declare a body far larger than the max. Don't actually send the
	// body — Read should reject before reading bytes.
	c := NewConn(ConnOptions{
		Reader:          strings.NewReader("Content-Length: 100000\r\n\r\n"),
		MaxPayloadBytes: 1024,
	})
	_, err := c.Read()
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("err = %v, want ErrPayloadTooLarge", err)
	}
}

func TestConn_Write_BodyTooLarge(t *testing.T) {
	var buf bytes.Buffer
	c := NewConn(ConnOptions{Writer: &buf, MaxPayloadBytes: 32})
	// 256 bytes of params well exceeds the 32-byte cap.
	big := bytes.Repeat([]byte("a"), 256)
	params, _ := json.Marshal(string(big))
	err := c.Write(&Notification{Method: "x", Params: params})
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("err = %v, want ErrPayloadTooLarge", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes; oversize Write must not touch the writer", buf.Len())
	}
}

func TestConn_Decode_BadVersion(t *testing.T) {
	body := `{"jsonrpc":"1.0","method":"ping"}`
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	c := NewConn(ConnOptions{Reader: strings.NewReader(frame)})
	_, err := c.Read()
	if !errors.Is(err, ErrBadVersion) {
		t.Errorf("err = %v, want ErrBadVersion", err)
	}
}

func TestConn_Decode_MissingVersion(t *testing.T) {
	body := `{"method":"ping"}`
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	c := NewConn(ConnOptions{Reader: strings.NewReader(frame)})
	_, err := c.Read()
	if !errors.Is(err, ErrBadVersion) {
		t.Errorf("err = %v, want ErrBadVersion", err)
	}
}

func TestConn_Decode_ResponseBothResultAndError(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":1,"error":{"code":-1,"message":"x"}}`
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	c := NewConn(ConnOptions{Reader: strings.NewReader(frame)})
	_, err := c.Read()
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("err = %v, want ErrMalformedMessage", err)
	}
}

func TestConn_Decode_ResponseNeitherResultNorError(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1}`
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	c := NewConn(ConnOptions{Reader: strings.NewReader(frame)})
	_, err := c.Read()
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("err = %v, want ErrMalformedMessage", err)
	}
}

func TestConn_Decode_MalformedJSON(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"x",}` // trailing comma
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	c := NewConn(ConnOptions{Reader: strings.NewReader(frame)})
	_, err := c.Read()
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("err = %v, want ErrMalformedMessage wrapper", err)
	}
}

// slowReader returns one byte at a time, simulating a pipe that hands
// the consumer headers character-by-character. The Conn must still
// reassemble them.
type slowReader struct {
	data []byte
	pos  int
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = s.data[s.pos]
	s.pos++
	return 1, nil
}

func TestConn_Decode_ChunkedHeader(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	c := NewConn(ConnOptions{Reader: &slowReader{data: []byte(frame)}})
	got, err := c.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, ok := got.(*Request); !ok {
		t.Errorf("got %T, want *Request", got)
	}
}

func TestConn_Read_EOFOnEmpty(t *testing.T) {
	c := NewConn(ConnOptions{Reader: strings.NewReader("")})
	_, err := c.Read()
	if !errors.Is(err, io.EOF) {
		t.Errorf("err = %v, want EOF wrapped", err)
	}
}

func TestConn_Read_TruncatedBody(t *testing.T) {
	// Declares Content-Length: 100 but the body is empty.
	c := NewConn(ConnOptions{Reader: strings.NewReader("Content-Length: 100\r\n\r\n")})
	_, err := c.Read()
	if err == nil {
		t.Fatal("Read: nil error, want unexpected EOF")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want io.ErrUnexpectedEOF wrapped", err)
	}
}

func TestConn_Write_ConcurrentWrites_Race(t *testing.T) {
	const writers = 16
	const perWriter = 25
	const total = writers * perWriter

	var buf bytes.Buffer
	c := NewConn(ConnOptions{Writer: &buf})

	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				params := json.RawMessage(fmt.Sprintf(`{"i":%d,"j":%d}`, i, j))
				if err := c.Write(&Notification{Method: "test", Params: params}); err != nil {
					t.Errorf("Write: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	// Now read everything back. The bytes.Buffer accumulated all writes
	// — pipe a fresh Conn at it and decode total messages. If framing
	// got interleaved we will see decode errors or wrong (i,j) pairs.
	reader := NewConn(ConnOptions{Reader: &buf})
	seen := make(map[[2]int]bool, total)
	for k := 0; k < total; k++ {
		msg, err := reader.Read()
		if err != nil {
			t.Fatalf("Read at frame %d/%d: %v", k, total, err)
		}
		n, ok := msg.(*Notification)
		if !ok {
			t.Fatalf("frame %d: got %T, want *Notification", k, msg)
		}
		var p struct {
			I int `json:"i"`
			J int `json:"j"`
		}
		if err := json.Unmarshal(n.Params, &p); err != nil {
			t.Fatalf("frame %d: bad params %s: %v", k, n.Params, err)
		}
		if seen[[2]int{p.I, p.J}] {
			t.Errorf("frame %d: duplicate (%d,%d)", k, p.I, p.J)
		}
		seen[[2]int{p.I, p.J}] = true
	}
	for i := 0; i < writers; i++ {
		for j := 0; j < perWriter; j++ {
			if !seen[[2]int{i, j}] {
				t.Errorf("missing (%d,%d)", i, j)
			}
		}
	}
}

// ID type tests --------------------------------------------------------

func TestID_Marshal_Int(t *testing.T) {
	data, err := json.Marshal(NewIntID(42))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(data) != "42" {
		t.Errorf("Marshal = %s, want 42", data)
	}
}

func TestID_Marshal_String(t *testing.T) {
	data, err := json.Marshal(NewStringID("abc"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(data) != `"abc"` {
		t.Errorf("Marshal = %s, want %q", data, `"abc"`)
	}
}

func TestID_Unmarshal_Int(t *testing.T) {
	var id ID
	if err := json.Unmarshal([]byte("100"), &id); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	n, ok := id.Int64()
	if !ok || n != 100 {
		t.Errorf("Int64 = (%d, %v), want (100, true)", n, ok)
	}
}

func TestID_Unmarshal_String(t *testing.T) {
	var id ID
	if err := json.Unmarshal([]byte(`"req-1"`), &id); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	s, ok := id.AsString()
	if !ok || s != "req-1" {
		t.Errorf("AsString = (%q, %v), want (\"req-1\", true)", s, ok)
	}
}

func TestID_Unmarshal_Null(t *testing.T) {
	var id ID
	if err := json.Unmarshal([]byte("null"), &id); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !id.IsNull() {
		t.Errorf("IsNull = false, want true")
	}
	if _, ok := id.Int64(); ok {
		t.Errorf("Int64 reported ok on null ID")
	}
	if _, ok := id.AsString(); ok {
		t.Errorf("AsString reported ok on null ID")
	}
}

func TestID_Unmarshal_RejectsFloat(t *testing.T) {
	var id ID
	err := json.Unmarshal([]byte("1.5"), &id)
	if err == nil {
		t.Error("Unmarshal accepted float; want error")
	}
}

func TestID_Unmarshal_RejectsObject(t *testing.T) {
	var id ID
	err := json.Unmarshal([]byte("{}"), &id)
	if err == nil {
		t.Error("Unmarshal accepted object; want error")
	}
}

// ResponseError satisfies the error interface.
func TestResponseError_Error_NilSafe(t *testing.T) {
	var e *ResponseError
	got := e.Error()
	if !strings.Contains(got, "nil") {
		t.Errorf("nil receiver Error() = %q, want it to mention nil", got)
	}
}
