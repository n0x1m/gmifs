package main

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/n0x1m/gmifs/gemini"
)

const (
	// Input                     = 10
	// SensitiveInput            = 11
	Success           = 20
	RedirectTemporary = 30
	RedirectPermanent = 31
	TemporaryFailure  = 40
	ServerUnavailable = 41
	// CgiError                  = 42
	// ProxyError                = 43
	// SlowDown                  = 44
	// PermanentFailure = 50
	NotFound = 51
	// Gone                      = 52
	ProxyRequestRefused = 53
	BadRequest          = 59
	// ClientCertificateRequired = 60
	// CertificateNotAuthorized  = 61
	// CertificateNotValid       = 62
)

const (
	Termination = "\r\n"
	URLMaxBytes = 1024
	IndexGmi    = "index.gmi"
	GeminiMIME  = "text/gemini"

	DefaultAddress  = ":1965"
	DefaultMaxConns = 256
	DefaultTimeout  = 10
	DefaultRootPath = "/var/www/htdocs/gemini"
	DefaultCN       = ""
	DefaultCertPath = ""
	DefaultKeyPath  = ""
)

func main() {
	var address, root, crt, key, cn string
	var maxconns, timeout int

	flag.StringVar(&address, "address", DefaultAddress, "address to listen on. E.g. 127.0.0.1:1965")
	flag.IntVar(&maxconns, "max-conns", DefaultMaxConns, "maximum number of concurrently open connections")
	flag.IntVar(&timeout, "timeout", DefaultTimeout, "connection timeout in seconds")
	flag.StringVar(&root, "root", DefaultRootPath, "server root directory to serve from")
	flag.StringVar(&cn, "cn", DefaultCN, "x509 Common Name when using temporary self-signed certs")
	flag.StringVar(&crt, "cert", DefaultCertPath, "TLS chain of one or more certificates")
	flag.StringVar(&key, "key", DefaultKeyPath, "TLS private key")
	flag.Parse()

	var err error
	var cert tls.Certificate
	if crt != "" && key != "" {
		log.Println("loading certificate from", crt)
		cert, err = tls.LoadX509KeyPair(crt, key)
		if err != nil {
			log.Fatalf("server: loadkeys: %s", err)
		}
	} else if cn != "" {
		log.Println("generating self-signed temporary certificate")
		cert, err = gemini.GenX509KeyPair(cn)
		if err != nil {
			log.Fatalf("server: loadkeys: %s", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "need either a keypair with cert and key or a common name (hostname)\n")
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	/*
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		go func() {
			for {
				<-hup
			}
		}()
	*/

	config := &tls.Config{
		Certificates:             []tls.Certificate{cert},
		Rand:                     rand.Reader,
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
		},
	}
	listener, err := tls.Listen("tcp", address, config)
	if err != nil {
		log.Fatalf("server: listen: %s", err)
	}

	queue := make(chan net.Conn, maxconns)
	go func() {
		type semaphore chan struct{}
		sem := make(semaphore, maxconns)
		for {
			// for each connection we receive
			conn := <-queue
			sem <- struct{}{} // acquire
			go func() {
				defer func() {
					conn.Close()
					<-sem // release
				}()
				handler := make(chan Response)
				go handleConnectionChannel(conn, root, handler)
				select {
				case rsp := <-handler:
					response, err := rsp.res, rsp.err

					var gmierr *GmiError
					if err != nil && errors.As(err, &gmierr) {
						if gmierr.Code == RedirectPermanent || gmierr.Code == RedirectTemporary {
							// error is relative path if redirect
							redirect := "gemini://" + cn + err.Error()
							sendError(conn, gmierr.Code, redirect)

							return
						}
						sendError(conn, gmierr.Code, err.Error())

						return
					}

					sendFile(conn, response.file, response.mimeType)
					response.file.Close()
				case <-time.After(10 * time.Second):
					sendError(conn, ServerUnavailable, "Server Unavailable")
				}
			}()
		}
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("server: accept: %s", err)
			break
		}
		queue <- conn
	}
}

type Response struct {
	res File
	err error
}

type File struct {
	file     *os.File
	mimeType string
}

type GmiError struct {
	Code int
	err  error
}

func Error(code int, err error) error {
	return &GmiError{Code: code, err: err}
}

func (e *GmiError) Error() string {
	return e.err.Error()
}

func (e *GmiError) Unwrap() error {
	return e.err
}

func handleConnectionChannel(c net.Conn, root string, rsp chan Response) {
	result, err := handleConnection(c, root)
	if err != nil {
		rsp <- Response{err: err}
		return
	}
	rsp <- Response{res: *result, err: err}
}

func handleConnection(c net.Conn, root string) (*File, error) {
	req, err := bufio.NewReader(c).ReadString('\r')
	if err != nil {
		return nil, Error(TemporaryFailure, errors.New("error reading request"))
	}
	fmt.Printf("%s\n", req)

	requestURL := strings.TrimSpace(req)
	if requestURL == "" {
		return nil, Error(BadRequest, errors.New("empty request URL"))
	} else if !utf8.ValidString(requestURL) {
		return nil, Error(BadRequest, errors.New("not a valid utf-8 url"))
	} else if len(requestURL) > URLMaxBytes {
		return nil, Error(BadRequest, errors.New("url exceeds maximum allowed length"))
	}

	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return nil, Error(BadRequest, err)
	}

	if parsedURL.Scheme == "" {
		parsedURL.Scheme = "gemini"
	}
	if parsedURL.Scheme != "gemini" {
		return nil, Error(ProxyRequestRefused, fmt.Errorf("unknown protocol scheme %s", parsedURL.Scheme))
	} else if parsedURL.Host == "" {
		return nil, Error(BadRequest, errors.New("empty host"))
	}

	_, port, _ := net.SplitHostPort(c.LocalAddr().String())
	if parsedURL.Port() != "" && parsedURL.Port() != port {
		return nil, Error(ProxyRequestRefused, errors.New("faulty port"))
	}

	return handleRequest(c, root, parsedURL)
}

func handleRequest(c net.Conn, root string, parsedURL *url.URL) (*File, error) {
	if parsedURL.Path == "" {
		return nil, Error(RedirectPermanent, errors.New(parsedURL.Path))
	} else if parsedURL.Path != path.Clean(parsedURL.Path) {
		return nil, Error(BadRequest, errors.New("path error"))
	}

	if parsedURL.Path == "/" || parsedURL.Path == "." {
		return serveFile(root, IndexGmi)
	}
	return serveFile(root, parsedURL.Path)
}

func serveFile(root, filepath string) (*File, error) {
	fullPath := path.Join(root, filepath)

	pathInfo, err := os.Stat(fullPath)
	if err != nil {
		return nil, Error(NotFound, err)
	}

	if pathInfo.IsDir() {
		subDirIndex := path.Join(fullPath, IndexGmi)
		if _, err := os.Stat(subDirIndex); os.IsNotExist(err) {
			return nil, Error(NotFound, err)
		}

		fullPath = subDirIndex
	}

	mimeType := getMimeType(fullPath)
	if mimeType == "" {
		return nil, Error(NotFound, errors.New("unsupported"))
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return nil, Error(NotFound, err)
	}

	return &File{file: file, mimeType: mimeType}, nil
}

func sendError(c net.Conn, code int, message string) {
	c.Write(header(code, message))
}

func sendFile(c net.Conn, file *os.File, mimeType string) {
	if file != nil {
		c.Write(header(Success, mimeType))
		io.Copy(c, file)
		return
	}
	sendError(c, TemporaryFailure, "file handler failed")
}

func header(code int, message string) []byte {
	// <STATUS><SPACE><META><CR><LF>
	if len(message) == 0 {
		return []byte(fmt.Sprintf("%d%s", code, Termination))
	}
	return []byte(fmt.Sprintf("%d %s%s", code, message, Termination))
}

func getMimeType(fullPath string) string {
	if ext := path.Ext(fullPath); ext != ".gmi" {
		return mime.TypeByExtension(ext)
	}
	return GeminiMIME
}
