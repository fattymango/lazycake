//go:build integration

package quic

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/fattymango/lazycake/internal/transport"
)

func relayCert(t *testing.T, ip string) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: ip}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP(ip)}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, leaf
}

func udpAddr(t *testing.T) string {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().String()
}

// Agents and gateways send their tokens to the relay. With TLS on, the relay's certificate must be the trusted one: a different certificate (a
// man in the middle) is refused before any token is sent.
func TestRelayCertificateIsVerifiedByClients(t *testing.T) {
	t.Setenv("LAZYCAKE_COORDINATOR_TLS", "1")
	trusted, trustedLeaf := relayCert(t, "127.0.0.1")
	impostor, _ := relayCert(t, "127.0.0.1")
	pool := x509.NewCertPool()
	pool.AddCert(trustedLeaf)
	transport.RootCAs = pool
	t.Cleanup(func() { transport.RootCAs = nil })

	dial := func(cert tls.Certificate) error {
		addr := udpAddr(t)
		relay := &Relay{Auth: fakeAuth{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			TLSConfig: func() *tls.Config { return &tls.Config{Certificates: []tls.Certificate{cert}} }}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = relay.Serve(ctx, addr) }()
		time.Sleep(150 * time.Millisecond)
		dctx, dcancel := context.WithTimeout(ctx, 3*time.Second)
		defer dcancel()
		conn, err := DialAgent(dctx, addr, "agent-token")
		if err == nil {
			_ = conn.CloseWithError(0, "")
		}
		return err
	}
	if err := dial(trusted); err != nil {
		t.Fatalf("the trusted certificate must be accepted: %v", err)
	}
	if err := dial(impostor); err == nil {
		t.Fatal(fmt.Sprint("a certificate the client does not trust must be refused"))
	}
}
