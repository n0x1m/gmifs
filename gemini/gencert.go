package gemini

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"time"
)

const rsaBits = 2048

// GenX509KeyPair generates a TLS keypair with one week validity.
func GenX509KeyPair(host string, daysvalid int) (tls.Certificate, error) {
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(now.Unix()),
		Subject: pkix.Name{
			CommonName:   host,
			Organization: []string{host},
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(0, 0, daysvalid),
		BasicConstraintsValid: true,
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage: x509.KeyUsageKeyEncipherment |
			x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}

	priv, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}

	cert, err := x509.CreateCertificate(rand.Reader, template, template,
		priv.Public(), priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate: %w", err)
	}

	var out tls.Certificate
	out.Certificate = append(out.Certificate, cert)
	out.PrivateKey = priv

	return out, nil
}
