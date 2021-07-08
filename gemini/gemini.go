package gemini

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	StatusInput                     = 10
	StatusSensitiveInput            = 11
	StatusSuccess                   = 20
	StatusRedirectTemporary         = 30
	StatusRedirectPermanent         = 31
	StatusTemporaryFailure          = 40
	StatusServerUnavailable         = 41
	StatusCgiError                  = 42
	StatusProxyError                = 43
	StatusSlowDown                  = 44
	StatusPermanentFailure          = 50
	StatusNotFound                  = 51
	StatusGone                      = 52
	StatusProxyRequestRefused       = 53
	StatusBadRequest                = 59
	StatusClientCertificateRequired = 60
	StatusCertificateNotAuthorized  = 61
	StatusCertificateNotValid       = 62
)

const (
	Termination = "\r\n"
	URLMaxBytes = 1024
	IndexFile   = "index.gmi"
	MimeType    = "text/gemini; charset=utf-8"
)

var (
	ErrServerClosed  = errors.New("gemini: server closed")
	ErrHeaderTooLong = errors.New("gemini: header too long")
	ErrMissingFile   = errors.New("gemini: no such file")
)

type Request struct {
	ctx        context.Context
	URL        *url.URL
	RemoteAddr string

	// RequestURI is the unmodified request-target of the Request-Line  as sent by the client
	// to a server. Usually the URL field should be used instead.
	RequestURI string
}

type Handler interface {
	ServeGemini(io.Writer, *Request)
}

// The HandlerFunc type is an adapter to allow the use of
// ordinary functions as Gemini handlers. If f is a function
// with the appropriate signature, HandlerFunc(f) is a
// Handler that calls f.
type HandlerFunc func(io.Writer, *Request)

// ServeGemini calls f(w, r).
func (f HandlerFunc) ServeGemini(w io.Writer, r *Request) {
	f(w, r)
}

type Server struct {
	// Addr is the address the server is listening on.
	Addr string

	// Hostname or common name of the server. This is used for absolute redirects.
	Hostname string

	// Logger enables logging of the gemini server for debugging purposes.
	Logger *log.Logger

	TLSConfig    *tls.Config
	Handler      Handler // handler to invoke
	ReadTimeout  time.Duration
	MaxOpenConns int
}

func (s *Server) log(v string) {
	if s.Logger == nil {
		return
	}
	s.Logger.Println("DEBUG " + v)
}

func (s *Server) logf(format string, v ...interface{}) {
	if s.Logger == nil {
		return
	}
	s.log(fmt.Sprintf(format, v...))
}

func (s *Server) ListenAndServe() error {
	// outer for loop, if listener closes we will restart it. This may be useful if we switch out
	// TLSConfig.
	//for {
	listener, err := tls.Listen("tcp", s.Addr, s.TLSConfig)
	if err != nil {
		return fmt.Errorf("gemini server listen: %w", err)
	}

	queue := make(chan net.Conn, s.MaxOpenConns)
	go s.handleConnectionQueue(queue)

	for {
		conn, err := listener.Accept()
		if err != nil {
			s.logf("server accept error: %v", err)
			break
		}
		queue <- conn
	}
	//}
	return nil
}

func (s *Server) handleConnectionQueue(queue chan net.Conn) {
	// semaphore for connection limiter
	type semaphore chan struct{}
	sem := make(semaphore, s.MaxOpenConns)
	for {
		// for each connection we receive
		conn := <-queue
		sem <- struct{}{} // acquire
		go s.handleConnection(conn, sem)
	}
}

func (s *Server) handleConnection(conn net.Conn, sem chan struct{}) {
	defer func() {
		conn.Close()
		<-sem // release
	}()
	reqChan := make(chan request)
	// push job for which we allocated a sem slot and wait
	go requestChannel(conn, reqChan)
	select {
	case header := <-reqChan:
		if header.err != nil {
			s.handleRequestError(conn, header)
			return
		}
		ctx := context.Background()
		r := &Request{
			ctx:        ctx,
			URL:        header.URL,
			RequestURI: header.rawuri,
			RemoteAddr: conn.RemoteAddr().String(),
		}
		s.Handler.ServeGemini(conn, r)
	case <-time.After(s.ReadTimeout):
		s.logf("server read timeout, request queue length %v/%v", len(sem), s.MaxOpenConns)
		WriteHeader(conn, StatusServerUnavailable, "")
	}
}

func (s *Server) handleRequestError(conn net.Conn, req request) {
	s.logf("server error: '%s' %v", strings.TrimSpace(req.rawuri), req.err)

	var gmierr *GmiError
	if errors.As(req.err, &gmierr) {
		WriteHeader(conn, gmierr.Code, gmierr.Error())
		return
	}

	// this path doesn't exist currently.
	WriteHeader(conn, StatusTemporaryFailure, "internal")
}

// conn handler

type request struct {
	rawuri string
	URL    *url.URL
	err    error
}

func requestChannel(c net.Conn, rsp chan request) {
	r, err := readHeader(c)
	r.err = err
	rsp <- *r
}

func readHeader(c net.Conn) (*request, error) {
	r := &request{}
	req, err := bufio.NewReader(c).ReadString('\r')
	if err != nil {
		return nil, Error(StatusTemporaryFailure, errors.New("error reading request"))
	}
	r.rawuri = req

	requestURL := strings.TrimSpace(req)
	if requestURL == "" {
		return r, Error(StatusBadRequest, errors.New("empty request URL"))
	} else if !utf8.ValidString(requestURL) {
		return r, Error(StatusBadRequest, errors.New("not a valid utf-8 url"))
	} else if len(requestURL) > URLMaxBytes {
		return r, Error(StatusBadRequest, ErrHeaderTooLong)
	}

	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return r, Error(StatusBadRequest, err)
	}

	r.URL = parsedURL

	if parsedURL.Scheme != "" && parsedURL.Scheme != "gemini" {
		return r, Error(StatusProxyRequestRefused, fmt.Errorf("unknown protocol scheme %s", parsedURL.Scheme))
	} else if parsedURL.Host == "" {
		return r, Error(StatusBadRequest, errors.New("empty host"))
	}

	if parsedURL.Path == "" {
		return r, Error(StatusRedirectPermanent, errors.New("./"+parsedURL.Path))
	} else if parsedURL.Path != path.Clean(parsedURL.Path) {
		return r, Error(StatusBadRequest, errors.New("path error"))
	}

	return r, nil
}

func (s *Server) Shutdown(ctx context.Context) error {

	return nil
}

func WriteHeader(c io.Writer, code int, message string) {
	// <STATUS><SPACE><META><CR><LF>
	var header []byte
	if len(message) == 0 {
		header = []byte(fmt.Sprintf("%d%s", code, Termination))
	}
	header = []byte(fmt.Sprintf("%d %s%s", code, message, Termination))
	c.Write(header)
}

func Write(c io.Writer, body []byte) {
	reader := bytes.NewReader(body)
	io.Copy(c, reader)
}
