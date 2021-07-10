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
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
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
	ErrServerClosed    = errors.New("gemini: server closed")
	ErrHeaderTooLong   = errors.New("gemini: header too long")
	ErrMissingFile     = errors.New("gemini: no such file")
	ErrEmptyRequest    = errors.New("gemini: empty request")
	ErrEmptyRequestURL = errors.New("gemini: empty request URL")
	ErrInvalidPath     = errors.New("gemini: path error")
	ErrInvalidHost     = errors.New("gemini: empty host")
	ErrInvalidUtf8     = errors.New("gemini: empty request URL")
	ErrUnknownProtocol = fmt.Errorf("gemini: unknown protocol scheme")
)

type Request struct {
	ctx        context.Context
	URL        *url.URL
	RemoteAddr string

	// RequestURI is the unmodified request-target of the Request-Line  as sent by the client
	// to a server. Usually the URL field should be used instead.
	RequestURI string
}

type ResponseWriter interface {
	WriteHeader(code int, message string) (int, error)
	Write(body []byte) (int, error)
}

type Handler interface {
	ServeGemini(ResponseWriter, *Request)
}

// The HandlerFunc type is an adapter to allow the use of
// ordinary functions as Gemini handlers. If f is a function
// with the appropriate signature, HandlerFunc(f) is a
// Handler that calls f.
type HandlerFunc func(ResponseWriter, *Request)

// ServeGemini calls f(w, r).
func (f HandlerFunc) ServeGemini(w ResponseWriter, r *Request) {
	f(w, r)
}

type Server struct {
	// Addr is the address the server is listening on.
	Addr string

	// Hostname or common name of the server. This is used for absolute redirects.
	Hostname string

	// Logger enables logging of the gemini server for debugging purposes.
	Logger *log.Logger

	TLSConfig       *tls.Config
	TLSConfigLoader func() (*tls.Config, error)

	Handler      Handler // handler to invoke
	ReadTimeout  time.Duration
	MaxOpenConns int

	// internal
	listener       net.Listener
	shutdown       bool
	closed         chan struct{}
	sighupListener chan struct{}
}

func (s *Server) log(v string) {
	if s.Logger == nil {
		return
	}

	s.Logger.Println("gmifs: " + v)
}

func (s *Server) logf(format string, v ...interface{}) {
	if s.Logger == nil {
		return
	}

	s.log(fmt.Sprintf(format, v...))
}

func (s *Server) loadTLS() (err error) {
	s.TLSConfig, err = s.TLSConfigLoader()
	return err
}

func (s *Server) reloadTLSConfigOnSighup() {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)

	for {
		select {
		case <-hup:

			s.log("reloading certificate")
			if s.listener != nil {
				err := s.loadTLS()
				if err != nil {
					fmt.Fprintf(os.Stderr, "critical: failed to load tls certs: %v", err)
					os.Exit(1)
				}

				s.listener.Close()
			}
		case <-s.closed:
			close(s.sighupListener)
			return
		}
	}
}

func (s *Server) ListenAndServe() error {
	err := s.loadTLS()
	if err != nil {
		return err
	}

	s.sighupListener = make(chan struct{})
	go s.reloadTLSConfigOnSighup()

	// outer for loop, if listener closes we will restart it. This may be useful if we switch out
	// TLSConfig.
	for {
		s.closed = make(chan struct{})

		var err error

		s.listener, err = tls.Listen("tcp", s.Addr, s.TLSConfig)
		if err != nil {
			return fmt.Errorf("gemini server listen: %w", err)
		}

		queue := make(chan net.Conn, s.MaxOpenConns)
		go s.handleConnectionQueue(queue)

		s.logf("Accepting new connections on %v", s.listener.Addr())
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				s.logf("server accept error: %v", err)

				break
			}
			queue <- conn

			// un-stuck call after shutdown will trigger a drop here
			if s.shutdown {
				break
			}
		}

		// closed confirms the accept call stopped
		close(s.closed)
		if s.shutdown {
			break
		}
	}

	s.log("closing listener gracefully")
	return s.listener.Close()
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
	w := &writer{conn}

	// push job for which we allocated a sem slot and wait
	go requestChannel(conn, reqChan)
	select {
	case header := <-reqChan:
		if header.err != nil {
			s.handleRequestError(conn, w, header)

			return
		}

		ctx := context.Background()
		r := &Request{
			ctx:        ctx,
			URL:        header.URL,
			RequestURI: header.rawuri,
			RemoteAddr: conn.RemoteAddr().String(),
		}

		s.Handler.ServeGemini(w, r)

	case <-time.After(s.ReadTimeout):
		s.logf("server read timeout, request queue length %v/%v", len(sem), s.MaxOpenConns)
		w.WriteHeader(StatusServerUnavailable, "")
	}
}

func (s *Server) handleRequestError(conn net.Conn, w ResponseWriter, req request) {
	if errors.Is(req.err, ErrEmptyRequest) {
		// in debug mode we log these too
		s.logf("empty request ignored - %v", conn.RemoteAddr().String())
		return
	}

	var gmierr *GmiError
	if errors.As(req.err, &gmierr) {
		// notify if error or redirect
		if gmierr.Code == StatusRedirectPermanent || gmierr.Code == StatusRedirectTemporary {
			s.logf("redirect '%s' -> '%s' %d - %s",
				strings.TrimSpace(req.URL.Path), req.err, gmierr.Code, conn.RemoteAddr().String())
		} else {
			s.logf("read request error: '%s' %v %d - %s",
				strings.TrimSpace(req.rawuri), req.err, gmierr.Code, conn.RemoteAddr().String())
		}

		w.WriteHeader(gmierr.Code, gmierr.Error())

		return
	}

	// this path doesn't exist currently.
	s.logf("unexpected error: '%s' %v - %s",
		strings.TrimSpace(req.rawuri), req.err, conn.RemoteAddr().String())
	w.WriteHeader(StatusTemporaryFailure, "internal")
}

// conn handler

type request struct {
	rawuri string
	URL    *url.URL
	err    error
}

func requestChannel(c net.Conn, rsp chan request) {
	req := &request{}

	r, err := readHeader(c)
	if r != nil {
		req = r
	}

	req.err = err
	rsp <- *req
}

func readHeader(c net.Conn) (*request, error) {
	req, err := bufio.NewReader(c).ReadString('\r')
	if err != nil {
		// not sure this is the right response
		return nil, Error(StatusTemporaryFailure, ErrEmptyRequest)
	}

	r := &request{}
	r.rawuri = req

	requestURL := strings.TrimSpace(req)
	if requestURL == "" {
		return r, Error(StatusBadRequest, ErrEmptyRequestURL)
	} else if len(requestURL) > URLMaxBytes {
		return r, Error(StatusBadRequest, ErrHeaderTooLong)
	} else if !utf8.ValidString(requestURL) {
		return r, Error(StatusBadRequest, ErrInvalidUtf8)
	}

	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return r, Error(StatusBadRequest, err)
	}

	r.URL = parsedURL
	return validateRequest(r)
}

func validateRequest(r *request) (*request, error) {
	if r.URL.Scheme != "" && r.URL.Scheme != "gemini" {
		return r, Error(StatusProxyRequestRefused, ErrUnknownProtocol)
	} else if r.URL.Host == "" {
		return r, Error(StatusBadRequest, ErrInvalidHost)
	}

	if r.URL.Path == "" {
		// This error is a redirect path.
		return r, Error(StatusRedirectPermanent, errors.New("./"+r.URL.Path))
	} else if cleaned := path.Clean(r.URL.Path); cleaned != r.URL.Path {
		// check valid alternative if unclean for directories
		if cleaned != strings.TrimRight(r.URL.Path, "/") {
			return r, Error(StatusBadRequest, ErrInvalidPath)
		}
	}

	return r, nil
}

// Shutdown uses the self-pipe trick to gracefully allow the accept handler to exit and the listener
// to close within the given context deadline. If unsuccessful the listener is forcefully
// terminated.
func (s *Server) Shutdown(ctx context.Context) error {
	s.log("shutdown request received")
	t := time.Now()
	go func() {
		s.shutdown = true
		// un-stuck call to self
		conn, err := tls.Dial("tcp", s.Addr, &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			s.logf("un-stuck call failed (ok): %v", err)

			return
		}
		defer conn.Close()
	}()

	select {
	case <-s.closed:
		s.log("all clients exited")
	case <-ctx.Done():
		s.logf("shutdown: context deadline exceeded after %v, terminating listener", time.Since(t))
		if err := s.listener.Close(); err != nil {
			s.logf("error while closing listener %v", err)
		}
	}
	// confirm sighup listener for cert reloading exited
	<-s.sighupListener

	return nil
}

type writer struct {
	w io.Writer
}

func (w *writer) WriteHeader(code int, message string) (int, error) {
	// <STATUS><SPACE><META><CR><LF>
	if len(message) == 0 {
		return w.Write([]byte(fmt.Sprintf("%d%s", code, Termination)))
	}

	return w.Write([]byte(fmt.Sprintf("%d %s%s", code, message, Termination)))
}

func (w *writer) Write(body []byte) (int, error) {
	reader := bytes.NewReader(body)
	n, err := io.Copy(w.w, reader)
	return int(n), err
}
