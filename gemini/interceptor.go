package gemini

import (
	"bytes"
)

// Interceptor is a ResponseWriter wrapper that may be used as buffer.
//
// A middleware may pass it to the next handlers ServeGemini method as a drop in replacement for the
// response writer. After the ServeGemini method is run the middleware may examine what has been
// written to the Interceptor and decide what to write to the "original" ResponseWriter (that may well be
// another buffer passed from another middleware).
//
// The downside is the body being written two times and the complete caching of the
// body in the memory.
type Interceptor struct {
	// ioWriter is the underlying response writer that is wrapped by Interceptor
	w ResponseWriter

	// Interceptor is the underlying io.Writer that buffers the response body
	Body bytes.Buffer

	// <STATUS><SPACE><META><CR><LF>

	// Code is the status code.
	Code int

	// Meta is the header addition, such as the mimetype.
	Meta string

	hasHeader bool
	hasBody   bool
}

// NewInterceptor creates a new Interceptor by wrapping the given response writer.
func NewInterceptor(w ResponseWriter) (m *Interceptor) {
	m = &Interceptor{}
	m.w = w
	return
}

// WriteHeader writes the cached status code and tracks this call as change
func (m *Interceptor) WriteHeader(code int, message string) (int, error) {
	m.hasHeader = true
	m.Code = code
	m.Meta = message
	return 0, nil
}

// Write writes to the underlying buffer and tracks this call as change
func (m *Interceptor) Write(body []byte) (int, error) {
	m.hasBody = true
	return m.Body.Write(body)
}

func (m *Interceptor) HasHeader() bool {
	return m.hasHeader
}

func (m *Interceptor) HasBody() bool {
	return m.hasBody
}

// FlushAll flushes headers, status code and body to the underlying ResponseWriter.
func (m *Interceptor) Flush() {
	m.FlushHeader()
	m.FlushBody()
}

// FlushBody flushes to the underlying responsewriter.
func (m *Interceptor) FlushBody() {
	m.w.Write(m.Body.Bytes())
}

// FlushHeader writes the header to the underlying ResponseWriter.
func (m *Interceptor) FlushHeader() {
	m.w.WriteHeader(m.Code, m.Meta)
}
