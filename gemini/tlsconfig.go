package gemini

import (
	"crypto/rand"
	"crypto/tls"
)

func TLSConfig(sni string, cert tls.Certificate) *tls.Config {
	return &tls.Config{
		ServerName:               sni,
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
}
