# gmifs

Gemini File Server, short gmifs, is intended to be minimal and serve static files. It is used
to accompany a hugo blog served via httpd and makes it available via the [gemini
protocol](https://gemini.circumlunar.space/docs/specification.gmi). Why built yet another gemini
server? Because it's educational and that's the spirit of the protocol.

**Features**
- **zero conf**, if no certificate is available, gmifs generates a self-signed cert
- **zero dependencies**, Go standard library only
- directory listing support through the auto index flag
- reloads ssl certs and reopens log files on SIGHUP, e.g. after Let's Encrypt renewal
- response writer interceptor and middleware support
- simple middleware for lru document cache
- concurrent request limiter
- KISS, single file gemini implementation, handler func in main
- modern tls ciphers (from [Mozilla's TLS ciphers recommendations](https://statics.tls.security.mozilla.org/server-side-tls-conf.json))

## Usage

### Installation

Currently only supported through the go toolchain, either check out the repot and build it or use:

```
go install github.com/n0x1m/gmifs
```

### Dev & Tests

Test it locally by serving e.g. a `./public` directory on localhost with directory listing turned on

```
./gmifs -root ./public -autoindex
```

If no key pair with the flags `-cert` and `-key` is provided, like in this example, gmifs will auto
provision a self-signed certificate for the hostname `localhost` with 1 day validity.

### Production

In the real world generate a self-signed server certificate with OpenSSL or use a Let's Encrypt
key pair. Generate example:

```bash
openssl req -x509 -newkey rsa:4096 -keyout key.rsa -out cert.pem \
     -days 3650 -nodes -subj "/CN=nox.im"
```

start gmifs with a Let's Encrypt key pair on OpenBSD:

```
gmifs -addr 0.0.0.0:1965 -root /var/www/htdocs/nox.im/gemini \
    -host nox.im -max-conns 256 -timeout 5 -cache 256 \
    -logs /var/www/logs/gemini \
    -cert /etc/ssl/nox.im.fullchain.pem \
    -key /etc/ssl/private/nox.im.key
```

if need be, send SIGHUP to reload the certificate without cold start, e.g. after certificate renewal

```
pgrep gmifs | awk '{print "kill -1 " $1}' | sh
```

If debug logs are enabled, the certificate rotation will be confirmed.

### Supported flags

```
Usage of ./gmifs:
  -addr string
        address to listen on, e.g. 127.0.0.1:1965 (default ":1965")
  -autocertvalidity int
        valid days when using a gmifs auto provisioned self-signed certificate (default 1)
  -autoindex
        enables auto indexing, directory listings
  -cache int
        simple lru document cache for n items. Disabled when zero.
  -cert string
        TLS chain of one or more certificates
  -debug
        enable verbose logging of the gemini server
  -host string
        hostname for sni and x509 CN when using temporary self-signed certs (default "localhost")
  -key string
        TLS private key
  -logs string
        enables file based logging and specifies the directory
  -max-conns int
        maximum number of concurrently open connections (default 128)
  -root string
        server root directory to serve from (default "public")
  -timeout int
        connection timeout in seconds (default 5)
```
