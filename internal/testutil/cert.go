package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"
)

// Cert is a self-signed certificate for 127.0.0.1, ::1 and localhost,
// generated per test so no private key is ever committed.
type Cert struct {
	TLS tls.Certificate
	PEM []byte // certificate only, as Bridge exports it
}

// NewCert generates a certificate.
func NewCert(t testing.TB) *Cert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		// The same subject as Bridge's own certificate.
		Subject: pkix.Name{
			Country: []string{"CH"}, Organization: []string{"Proton AG"},
			OrganizationalUnit: []string{"Proton Mail"}, CommonName: "127.0.0.1",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &Cert{
		TLS: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		PEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}
