package quic

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/fattymango/lazycake/internal/transport"
)

// selfSignedServerTLSConfig generates a throwaway certificate for the
// relay's QUIC listener. Real transport confidentiality here doesn't
// depend on certificate validation - PLAN.md's design puts an inner Noise
// session between agent and gateway specifically because "QUIC's own TLS
// does not give end-to-end encryption through a relay"; the relay
// terminates this TLS layer by design, so it's the outer envelope, not a
// trust boundary between agent/gateway and the relay itself. Real
// deployment would still want a stable, pinned cert; that's a deferred
// hardening item (see OPEN_QUESTIONS.md), not a phase 2 concern.
func selfSignedServerTLSConfig() (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"lazycake relay"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * 365 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("creating certificate: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshalling key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("building key pair: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"lazycake-tunnel"},
	}, nil
}

// alpn is the application protocol both ends of the relay connection name.
const alpn = "lazycake-tunnel"

// clientTLSConfig is the dialer's TLS configuration for the relay at addr: verified against the relay's certificate unless TLS is off for that
// address (loopback development, or LAZYCAKE_COORDINATOR_TLS=0), in which case it skips verification as it always did - see the comment on
// selfSignedServerTLSConfig. See internal/transport for the rule.
func clientTLSConfig(addr string) *tls.Config { return transport.QUICConfig(addr, []string{alpn}) }
