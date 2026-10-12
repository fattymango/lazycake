package tlscert

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func selfSigned(t *testing.T, ip string, from time.Time, life time.Duration) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: ip}, NotBefore: from, NotAfter: from.Add(life),
		IPAddresses: []net.IP{net.ParseIP(ip)}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, leaf
}

// Both kinds of client use the same port: nothing that speaks plain HTTP today breaks, and https works beside it.
func TestOnePortAnswersHTTPAndHTTPS(t *testing.T) {
	cert, leaf := selfSigned(t, "127.0.0.1", time.Now().Add(-time.Minute), time.Hour)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var sawTLS []bool
	h := Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawTLS = append(sawTLS, r.TLS != nil)
		_, _ = io.WriteString(w, "hello")
	}), false, nil)
	srv := &http.Server{Handler: h, ConnContext: ConnContext, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		_ = srv.Serve(Listener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}))
	}()
	t.Cleanup(func() { _ = srv.Close() })
	addr := ln.Addr().String()

	resp, err := http.Get("http://" + addr + "/")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, "hello", string(body))

	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err = c.Get("https://" + addr + "/")
	require.NoError(t, err)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, "hello", string(body))
	require.Equal(t, []bool{false, true}, sawTLS, "handlers can tell which one it was (r.TLS)")
}

// Plain-HTTP visitors are sent to https once a certificate is in place, but the server's own checks on localhost are left alone.
func TestForceHTTPSRedirectsOutsidersOnceReady(t *testing.T) {
	ready := false
	h := Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") }), true, func() bool { return ready })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{Handler: h, ConnContext: ConnContext}
	go func() { _ = srv.Serve(Listener(ln, &tls.Config{})) }()
	t.Cleanup(func() { _ = srv.Close() })

	// Everything here connects from loopback, which is exempt: the redirect itself is exercised below with a non-loopback remote address.
	resp, err := http.Get("http://" + ln.Addr().String() + "/x")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "loopback (the server's own health check) is never redirected")

	ready = true
	r := httptest.NewRequest(http.MethodGet, "http://68.183.72.125:8080/a?b=1", nil)
	r.RemoteAddr = "203.0.113.9:5555"
	rec := httptest.NewRecorder()
	// Same wrapper, with a connection the listener produced (a lazyConn, non-TLS) in the context.
	r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &lazyConn{}))
	h.ServeHTTP(rec, r)
	require.Equal(t, http.StatusPermanentRedirect, rec.Code)
	require.Equal(t, "https://68.183.72.125:8080/a?b=1", rec.Header().Get("Location"))

	ready = false
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	require.Equal(t, http.StatusOK, rec.Code, "no redirect before there is a certificate to redirect to")
}

func TestNeedsRenewalAtHalfLife(t *testing.T) {
	now := time.Now()
	_, fresh := selfSigned(t, "127.0.0.1", now.Add(-time.Hour), 6*24*time.Hour)
	_, half := selfSigned(t, "127.0.0.1", now.Add(-3*24*time.Hour-time.Minute), 6*24*time.Hour)
	_, expired := selfSigned(t, "127.0.0.1", now.Add(-10*24*time.Hour), 6*24*time.Hour)
	require.False(t, needsRenewal(fresh, now))
	require.True(t, needsRenewal(half, now), "a six-day certificate is renewed after three days")
	require.True(t, needsRenewal(expired, now))
	require.True(t, needsRenewal(nil, now), "no certificate yet")
}

func TestChallengeHandlerAnswersTokensAndRedirectsEveryoneElse(t *testing.T) {
	m, err := New(Config{Host: "203.0.113.5", Dir: t.TempDir(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	require.NoError(t, err)
	m.tok.Store("tok123", "tok123.thumbprint")
	h := m.challengeHandler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://203.0.113.5/.well-known/acme-challenge/tok123", nil))
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "tok123.thumbprint", rec.Body.String())

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://203.0.113.5/.well-known/acme-challenge/other", nil))
	require.Equal(t, 404, rec.Code, "an unknown token is not answered")

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://203.0.113.5/login?x=1", nil))
	require.Equal(t, http.StatusPermanentRedirect, rec.Code)
	require.True(t, strings.HasPrefix(rec.Header().Get("Location"), "https://203.0.113.5:8080/login?x=1"), rec.Header().Get("Location"))
}

func TestLoadRefusesACertificateForAnotherAddress(t *testing.T) {
	dir := t.TempDir()
	m, err := New(Config{Host: "203.0.113.5", Dir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	require.NoError(t, err)
	cert, _ := selfSigned(t, "198.51.100.7", time.Now().Add(-time.Minute), time.Hour)
	key := cert.PrivateKey.(*ecdsa.PrivateKey)
	require.Error(t, m.install(cert.Certificate, key), "a certificate for another address is refused")
	require.False(t, m.Ready())
	_, statErr := os.Stat(filepath.Join(dir, "cert.pem"))
	require.True(t, os.IsNotExist(statErr), "and it is not written over the good one on disk")
}

func TestReadyOnlyWhileTheCertificateIsValid(t *testing.T) {
	dir := t.TempDir()
	m, err := New(Config{Host: "203.0.113.5", Dir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	require.NoError(t, err)
	require.False(t, m.Ready())
	cert, _ := selfSigned(t, "203.0.113.5", time.Now().Add(-time.Minute), time.Hour)
	require.NoError(t, m.install(cert.Certificate, cert.PrivateKey.(*ecdsa.PrivateKey)))
	require.True(t, m.Ready())
	m.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	require.False(t, m.Ready(), "an expired certificate is not ready")

	// A restart finds the certificate on disk.
	again, err := New(Config{Host: "203.0.113.5", Dir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	require.NoError(t, err)
	require.True(t, again.Ready())
	_, err = again.TLSConfig().GetCertificate(nil)
	require.NoError(t, err)
}
