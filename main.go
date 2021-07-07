package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"os"
	"path"
	"time"

	"github.com/n0x1m/gmifs/gemini"
)

const (
	defaultAddress  = ":1965"
	defaultMaxConns = 256
	defaultTimeout  = 10
	defaultRootPath = "/var/www/htdocs/gemini"
	defaultHost     = ""
	defaultCertPath = ""
	defaultKeyPath  = ""

	shutdownTimeout = 10 * time.Second
)

func main() {
	var addr, root, crt, key, host string
	var maxconns, timeout int

	flag.StringVar(&addr, "addr", defaultAddress, "address to listen on. E.g. 127.0.0.1:1965")
	flag.IntVar(&maxconns, "max-conns", defaultMaxConns, "maximum number of concurrently open connections")
	flag.IntVar(&timeout, "timeout", defaultTimeout, "connection timeout in seconds")
	flag.StringVar(&root, "root", defaultRootPath, "server root directory to serve from")
	flag.StringVar(&host, "host", defaultHost, "hostname / x509 Common Name when using temporary self-signed certs")
	flag.StringVar(&crt, "cert", defaultCertPath, "TLS chain of one or more certificates")
	flag.StringVar(&key, "key", defaultKeyPath, "TLS private key")
	flag.Parse()

	var err error
	var cert tls.Certificate
	if crt != "" && key != "" {
		log.Println("loading certificate from", crt)
		cert, err = tls.LoadX509KeyPair(crt, key)
		if err != nil {
			log.Fatalf("server: loadkeys: %s", err)
		}
	} else if host != "" {
		log.Println("generating self-signed temporary certificate")
		cert, err = gemini.GenX509KeyPair(host)
		if err != nil {
			log.Fatalf("server: loadkeys: %s", err)
		}
	}
	if host == "" {
		fmt.Fprintf(os.Stderr, "a keypair with cert and key or at least a common name (hostname) is required for sni\n")
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	mux := gemini.NewMux()
	mux.Use(logger)
	mux.Handle(gemini.HandlerFunc(fileserver(root)))

	server := &gemini.Server{
		Addr:         addr,
		Hostname:     host,
		TLSConfig:    gemini.TLSConfig(host, cert),
		Handler:      mux,
		MaxOpenConns: maxconns,
		ReadTimeout:  time.Duration(timeout) * time.Second,
	}

	//confirm := make(chan struct{}, 1)
	//go func() {
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, gemini.ErrServerClosed) {
		log.Fatal("ListenAndServe terminated unexpectedly")
	}

	//	close(confirm)
	//}()

	/*
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)
		<-stop

		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		if err := server.Shutdown(ctx); err != nil {
			cancel()
			log.Fatal("ListenAndServe shutdown")
		}

		<-confirm
		cancel()
	*/
	/*
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		go func() {
			for {
				<-hup
			}
		}()
	*/
}

func logger(next gemini.Handler) gemini.Handler {
	fn := func(w io.Writer, r *gemini.Request) {
		log.Println(r.URL)
	}
	return gemini.HandlerFunc(fn)
}

func fileserver(root string) func(w io.Writer, r *gemini.Request) {
	return func(w io.Writer, r *gemini.Request) {
		fullpath, err := fullPath(root, r.URL.Path)
		if err != nil {
			gemini.WriteHeader(w, gemini.StatusNotFound, err.Error())
			return
		}
		body, mimeType, err := readFile(fullpath)
		if err != nil {
			gemini.WriteHeader(w, gemini.StatusNotFound, err.Error())
			return
		}

		gemini.WriteHeader(w, gemini.StatusSuccess, mimeType)
		gemini.Write(w, body)
	}
}

func fullPath(root, requestPath string) (string, error) {
	if requestPath == "/" || requestPath == "." {
		return path.Join(root, gemini.IndexFile), nil
	}

	fullpath := path.Join(root, requestPath)

	pathInfo, err := os.Stat(fullpath)
	if err != nil {
		return "", fmt.Errorf("path: %w", err)
	}

	if pathInfo.IsDir() {
		subDirIndex := path.Join(fullpath, gemini.IndexFile)
		if _, err := os.Stat(subDirIndex); os.IsNotExist(err) {
			return "", fmt.Errorf("path: %w", err)
		}

		fullpath = subDirIndex
	}

	return fullpath, nil
}

func readFile(filepath string) ([]byte, string, error) {
	mimeType := getMimeType(filepath)
	if mimeType == "" {
		return nil, "", errors.New("unsupported")
	}

	file, err := os.Open(filepath)
	if err != nil {
		return nil, "", fmt.Errorf("file: %w", err)
	}
	defer file.Close()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, "", fmt.Errorf("read: %w", err)
	}
	return data, mimeType, nil
}

func getMimeType(fullpath string) string {
	if ext := path.Ext(fullpath); ext != ".gmi" {
		return mime.TypeByExtension(ext)
	}
	return gemini.MimeType
}
