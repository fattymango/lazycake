package tlscert

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/fattymango/lazycake/internal/transport"
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

func tlsListener(t *testing.T, opt Options, nextProtos ...string) (addr string, pool *x509.CertPool, ln net.Listener) {
	t.Helper()
	cert, leaf := selfSigned(t, "127.0.0.1", time.Now().Add(-time.Minute), time.Hour)
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	pool = x509.NewCertPool()
	pool.AddCert(leaf)
	ln = ListenerWith(inner, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12, NextProtos: nextProtos}, opt)
	t.Cleanup(func() { _ = ln.Close() })
	return inner.Addr().String(), pool, ln
}

// The gRPC port, once every client is updated: plaintext is closed, TLS still works.
func TestRequireTLSRefusesPlaintextAndKeepsTLS(t *testing.T) {
	var refused atomic.Int32
	addr, pool, ln := tlsListener(t, Options{RequireTLS: true, OnRefused: func(string) { refused.Add(1) }})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 64)
				n, _ := c.Read(buf)
				_, _ = c.Write(buf[:n])
			}()
		}
	}()

	raw, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	_, _ = raw.Write([]byte("GET / HTTP/1.1\r\n\r\n"))
	_ = raw.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := raw.Read(make([]byte, 16))
	require.Zero(t, n, "nothing is echoed to a plaintext client")
	require.Error(t, err, "its connection is closed")
	require.Eventually(t, func() bool { return refused.Load() == 1 }, 2*time.Second, 20*time.Millisecond, "and the refusal is reported")

	conn, err := tls.Dial("tcp", addr, &tls.Config{RootCAs: pool})
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte("hello"))
	require.NoError(t, err)
	got := make([]byte, 5)
	_, err = io.ReadFull(conn, got)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))
}

// A client that connects and says nothing must not hold a connection open, and the deadline a server sets on a connection (HTTP's header
// timeout, gRPC's handshake timeout) must survive the peek instead of being cleared by it.
func TestPeekDoesNotLoosenTheServersDeadlines(t *testing.T) {
	old := peekTimeout
	peekTimeout = 300 * time.Millisecond
	t.Cleanup(func() { peekTimeout = old })

	cert, _ := selfSigned(t, "127.0.0.1", time.Now().Add(-time.Minute), time.Hour)
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		ReadHeaderTimeout: 700 * time.Millisecond,
	}
	go func() {
		_ = srv.Serve(Listener(inner, &tls.Config{Certificates: []tls.Certificate{cert}}))
	}()
	t.Cleanup(func() { _ = srv.Close() })

	// closedWithin reads until the server closes the connection (it may send a 408 first) and reports whether that happened in time.
	closedWithin := func(c net.Conn, d time.Duration) bool {
		_ = c.SetReadDeadline(time.Now().Add(d))
		for {
			if _, err := c.Read(make([]byte, 512)); err != nil {
				var ne net.Error
				return !(errors.As(err, &ne) && ne.Timeout())
			}
		}
	}

	silent, err := net.Dial("tcp", inner.Addr().String())
	require.NoError(t, err)
	defer silent.Close()
	require.True(t, closedWithin(silent, 3*time.Second), "a client that sends nothing is dropped after the peek timeout")

	stalled, err := net.Dial("tcp", inner.Addr().String())
	require.NoError(t, err)
	defer stalled.Close()
	_, _ = stalled.Write([]byte("G")) // the first byte of a request, then nothing: slowloris
	require.True(t, closedWithin(stalled, 4*time.Second), "the HTTP header timeout still applies after the peek")
}

// What the agent really does: gRPC with the shared client credentials, to a listener that takes both kinds of connection.
func TestGRPCOverTheSamePortWithTheSharedCredentials(t *testing.T) {
	check := func(t *testing.T, opt Options, forceTLS string, wantOK bool) {
		addr, pool, ln := tlsListener(t, opt, "h2")
		gs := grpc.NewServer()
		healthpb.RegisterHealthServer(gs, health.NewServer())
		go func() { _ = gs.Serve(ln) }()
		t.Cleanup(gs.Stop)

		t.Setenv("LAZYCAKE_COORDINATOR_TLS", forceTLS)
		transport.RootCAs = pool
		t.Cleanup(func() { transport.RootCAs = nil })
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(transport.GRPCCredentials(addr)))
		require.NoError(t, err)
		defer conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
		if wantOK {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
	t.Run("TLS client, either mode", func(t *testing.T) { check(t, Options{}, "1", true) })
	t.Run("plaintext client while plaintext is allowed", func(t *testing.T) { check(t, Options{}, "0", true) })
	t.Run("plaintext client once TLS is required", func(t *testing.T) { check(t, Options{RequireTLS: true}, "0", false) })
	t.Run("TLS client once TLS is required", func(t *testing.T) { check(t, Options{RequireTLS: true}, "1", true) })
}
