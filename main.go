package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/n0x1m/gmifs/fileserver"
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

	autoCertDaysValid = 7
	shutdownTimeout   = 10 * time.Second
)

func main() {
	var addr, root, crt, key, host, logs string
	var maxconns, timeout, cache int
	var debug, autoindex bool

	flag.StringVar(&addr, "addr", defaultAddress, "address to listen on. E.g. 127.0.0.1:1965")
	flag.IntVar(&maxconns, "max-conns", defaultMaxConns, "maximum number of concurrently open connections")
	flag.IntVar(&timeout, "timeout", defaultTimeout, "connection timeout in seconds")
	flag.IntVar(&cache, "cache", 0, "simple lru document cache for n items. Disabled when zero.")
	flag.StringVar(&root, "root", defaultRootPath, "server root directory to serve from")
	flag.StringVar(&host, "host", defaultHost, "hostname / x509 Common Name when using temporary self-signed certs")
	flag.StringVar(&crt, "cert", defaultCertPath, "TLS chain of one or more certificates")
	flag.StringVar(&key, "key", defaultKeyPath, "TLS private key")
	flag.StringVar(&logs, "logs", "", "directory for file based logging")
	flag.BoolVar(&debug, "debug", false, "enable verbose logging of the gemini server")
	flag.BoolVar(&autoindex, "autoindex", false, "enables or disables the directory listing output")
	flag.Parse()

	var err error
	var flogger, dlogger *log.Logger
	flogger, err = setupLogger(logs, "access.log")
	if err != nil {
		log.Fatal(err)
	}

	if debug {
		dlogger, err = setupLogger(logs, "debug.log")
		if err != nil {
			log.Fatal(err)
		}
	}

	if host == "" {
		fmt.Fprintf(os.Stderr, "a keypair with cert and key or at least a common name (hostname) is required for sni\n")
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	logprefix := host + " "

	mux := gemini.NewMux()
	mux.Use(middleware.Logger(flogger, logprefix))
	mux.Use(middleware.Cache(cache))
	mux.Handle(gemini.HandlerFunc(fileserver.Serve(root, autoindex)))

	server := &gemini.Server{
		Addr:            addr,
		Hostname:        host,
		TLSConfigLoader: setupCertificate(crt, key, host),
		Handler:         mux,
		MaxOpenConns:    maxconns,
		ReadTimeout:     time.Duration(timeout) * time.Second,
		Logger:          dlogger,
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
		log.Fatalf("ListenAndServe shutdown with error: %v", err)
	}

	<-confirm
	cancel()
}

func setupCertificate(crt, key, host string) func() (*tls.Config, error) {
	return func() (*tls.Config, error) {
		if crt != "" && key != "" {
			cert, err := tls.LoadX509KeyPair(crt, key)
			if err != nil {
				return nil, fmt.Errorf("load x509 keypair: %w", err)
			}
			return gemini.TLSConfig(host, cert), nil
		}

		log.Println("generating self-signed temporary certificate")
		cert, err := gemini.GenX509KeyPair(host, autoCertDaysValid)
		if err != nil {
			return nil, fmt.Errorf("generate x509 keypair: %w", err)
		}
		return gemini.TLSConfig(host, cert), nil
	}
}

func setupLogger(dir, filename string) (*log.Logger, error) {
	logger := log.New(os.Stdout, "", log.LUTC|log.Ldate|log.Ltime)

	if dir != "" {
		// non 12factor stuff
		logpath := filepath.Join(dir, filename)
		_, err := setupFileLogging(logger, logpath)
		if err != nil {
			return logger, fmt.Errorf("setup logger: %w", err)
		}

		go func(logger *log.Logger, logpath string) {
			hup := make(chan os.Signal, 1)
			signal.Notify(hup, syscall.SIGHUP)

			for {
				<-hup
				logger.Println("rotating log file after SIGHUP")
				_, err := setupFileLogging(logger, logpath)
				if err != nil {
					log.Fatalf("failed to rotate log file: %v", err)
				}
			}
		}(logger, logpath)
	}

	return logger, nil
}

func setupFileLogging(logger *log.Logger, logpath string) (*os.File, error) {
	logfile, err := os.OpenFile(logpath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return logfile, fmt.Errorf("log file open: %w", err)
	}

	logger.SetOutput(logfile)
	return logfile, nil
}
