package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"time"

	"github.com/n0x1m/gmifs/gemini"
	"github.com/n0x1m/gmifs/middleware"
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

var ErrDirWithoutIndex = errors.New("path is directory without index.gmi")

func main() {
	var addr, root, crt, key, host, logs string
	var maxconns, timeout int
	var debug bool

	flag.StringVar(&addr, "addr", defaultAddress, "address to listen on. E.g. 127.0.0.1:1965")
	flag.IntVar(&maxconns, "max-conns", defaultMaxConns, "maximum number of concurrently open connections")
	flag.IntVar(&timeout, "timeout", defaultTimeout, "connection timeout in seconds")
	flag.StringVar(&root, "root", defaultRootPath, "server root directory to serve from")
	flag.StringVar(&host, "host", defaultHost, "hostname / x509 Common Name when using temporary self-signed certs")
	flag.StringVar(&crt, "cert", defaultCertPath, "TLS chain of one or more certificates")
	flag.StringVar(&key, "key", defaultKeyPath, "TLS private key")
	flag.StringVar(&logs, "logs", "", "directory for file based logging")
	flag.BoolVar(&debug, "debug", false, "enable verbose logging of the gemini server")
	flag.Parse()

	// TODO: rotate on SIGHUP
	mlogger := log.New(os.Stdout, "", log.LUTC|log.Ldate|log.Ltime)
	if logs != "" {
		logpath := filepath.Join(logs, "access.log")
		accessLog, err := os.OpenFile(logpath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			log.Fatal(err)
		}
		defer accessLog.Close()

		mlogger.SetOutput(accessLog)
	}

	var dlogger *log.Logger
	if debug {
		dlogger = log.New(os.Stdout, "", log.LUTC|log.Ldate|log.Ltime)

		if logs != "" {
			logpath := filepath.Join(logs, "debug.log")
			debugLog, err := os.OpenFile(logpath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
			if err != nil {
				log.Fatal(err)
			}
			defer debugLog.Close()

			dlogger.SetOutput(debugLog)
		}
	}

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
	mux.Use(middleware.Logger(mlogger))
	mux.Handle(gemini.HandlerFunc(fileserver(root, true)))

	server := &gemini.Server{
		Addr:         addr,
		Hostname:     host,
		TLSConfig:    gemini.TLSConfig(host, cert),
		Handler:      mux,
		MaxOpenConns: maxconns,
		ReadTimeout:  time.Duration(timeout) * time.Second,
		Logger:       dlogger,
	}

	confirm := make(chan struct{}, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, gemini.ErrServerClosed) {
			log.Fatalf("ListenAndServe terminated unexpectedly: %v", err)
		}
		close(confirm)
	}()

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

}

func fileserver(root string, dirlisting bool) func(w io.Writer, r *gemini.Request) {
	return func(w io.Writer, r *gemini.Request) {
		fullpath, err := fullPath(root, r.URL.Path)
		if err != nil {
			if err == ErrDirWithoutIndex && dirlisting {
				body, mimeType, err := listDirectory(fullpath, r.URL.Path)
				if err != nil {
					gemini.WriteHeader(w, gemini.StatusNotFound, err.Error())
					return
				}

				gemini.WriteHeader(w, gemini.StatusSuccess, mimeType)
				gemini.Write(w, body)
				return
			}
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
		//return path.Join(root, gemini.IndexFile), nil
	}

	fullpath := path.Join(root, requestPath)

	pathInfo, err := os.Stat(fullpath)
	if err != nil {
		return "", fmt.Errorf("path: %w", err)
	}

	if pathInfo.IsDir() {
		subDirIndex := path.Join(fullpath, gemini.IndexFile)
		if _, err := os.Stat(subDirIndex); os.IsNotExist(err) {
			return fullpath, ErrDirWithoutIndex
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

func listDirectory(fullpath, relpath string) ([]byte, string, error) {
	files, err := ioutil.ReadDir(fullpath)
	if err != nil {
		return nil, "", err
	}

	var out []byte
	parent := filepath.Dir(relpath)
	if relpath != "/" {
		out = append(out, []byte(fmt.Sprintf("Index of %s/\n\n", relpath))...)
		out = append(out, []byte(fmt.Sprintf("=> %s ..\n", parent))...)
	} else {
		out = append(out, []byte(fmt.Sprintf("Index of %s\n\n", relpath))...)
	}
	for _, f := range files {
		if relpath == "/" {
			out = append(out, []byte(fmt.Sprintf("=> %s\n", f.Name()))...)
		} else {
			out = append(out, []byte(fmt.Sprintf("=> %s/%s %s\n", relpath, f.Name(), f.Name()))...)
		}
	}

	return out, gemini.MimeType, nil
}
