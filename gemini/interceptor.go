package gemini

import (
	"bytes"
)

// Interceptor is a ResponseWriter wrapper that may be used as buffer.
//
// A middleware may pass it to the next handlers ServeGemini method as a drop in replacement for the
// response writer. See the logger and cache middlewares for examples.
//
// Note that the body being written two times and the complete caching of the body in the memory.
type Interceptor struct {
	// ResponseWriter is the underlying response writer that is wrapped by Interceptor
	responseWriter ResponseWriter

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
func NewInterceptor(responseWriter ResponseWriter) (m *Interceptor) {
	m = &Interceptor{}
	m.responseWriter = responseWriter

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
	m.responseWriter.Write(m.Body.Bytes())
}

// FlushHeader writes the header to the underlying ResponseWriter.
func (m *Interceptor) FlushHeader() {
	m.responseWriter.WriteHeader(m.Code, m.Meta)
}
