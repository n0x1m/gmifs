# gmifs

Gemini File Server, short gmifs, is intended to be minimal and serve static files only. It is used
to accompany a hugo blog served via httpd and make it available via the [gemini
protocol](https://gemini.circumlunar.space/docs/specification.gmi). Why built yet another gemini
server? Because it's educational and that's the spirit of the protocol.

Features
- zero conf
- zero dependencies (pure go, standard library only)
- only modern tls ciphers (from Mozilla's [TLS ciphers recommendation](https://statics.tls.security.mozilla.org/server-side-tls-conf.json))
- concurrent request limiter

This tool is used alongside the markdown to gemtext converter
[md2gmi](https://github.com/n0x1m/md2gmi).

Generate a self-signed server certificate with openssl:

```bash
openssl req -x509 -newkey rsa:4096 -keyout key.rsa -out cert.pem \
     -days 3650 -nodes -subj "/CN=nox.im"
```

## Usage

locally test it by serving a `./gemini` directory

```
./gmifs -root ./gemini
```

full example

```
gmifs -addr 0.0.0.0:1965 -root /var/www/htdocs/nox.im/gemini \
    -cert /etc/ssl/nox.im.fullchain.pem \
    -key /etc/ssl/private/nox.im.key
```
