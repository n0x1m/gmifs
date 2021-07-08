# gmifs

Gemini File Server, short gmifs, is intended to be minimal and serve static files. It is used
to accompany a hugo blog served via httpd and makes it available via the [gemini
protocol](https://gemini.circumlunar.space/docs/specification.gmi). Why built yet another gemini
server? Because it's educational and that's the spirit of the protocol.

Features
- **zero conf**, if no certificate is available, gmifs generates a self-signed cert
- **zero dependencies**, Go standard library only
- directory listing support `-autoindex`
- reloads ssl certs and reopens log files on SIGHUP, e.g. after Let's Encrypt renewal
- KISS, single file gemini implementation, handler func in main
- modern tls ciphers (from [Mozilla's TLS ciphers recommendations](https://statics.tls.security.mozilla.org/server-side-tls-conf.json))

This tool is used alongside the markdown to gemtext converter
[md2gmi](https://github.com/n0x1m/md2gmi).

## Usage

### Dev & Tests

Test it locally by serving e.g. a `./public` directory on localhost with directory listing turned on

```
./gmifs -root ./public -host localhost -autoindex
```

### Production

In the real world generate a self-signed server certificate with OpenSSL or use a Let's Encrypt
key pair

```bash
openssl req -x509 -newkey rsa:4096 -keyout key.rsa -out cert.pem \
     -days 3650 -nodes -subj "/CN=nox.im"
```

start gmifs with the keypair

```
gmifs -addr 0.0.0.0:1965 -root /var/www/htdocs/nox.im/gemini \
    -host nox.im -max-conns 1024 -timeout 5 \
    -logs /var/gemini/logs/ \
    -cert /etc/ssl/nox.im.fullchain.pem \
    -key /etc/ssl/private/nox.im.key
```

if need be, send SIGHUP to reload the certificate without downtime

```
pgrep gmifs | awk '{print "kill -1 " $1}' | sh
```
