// Package tlscert gives the coordinator HTTPS on its existing port without a reverse proxy: it obtains and renews a Let's Encrypt certificate for the
// server's IP address (or DNS name) itself, answers the http-01 validation on port 80, and serves TLS and plain HTTP on the same listener (mux.go).
// IP-address certificates are only issued under Let's Encrypt's "shortlived" profile (about 6 days), so renewal is routine: it happens at half life.
package tlscert

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/acme"
)

// LetsEncryptProduction is the default ACME directory.
const LetsEncryptProduction = "https://acme-v02.api.letsencrypt.org/directory"

type Config struct {
	// Host is the IP address or DNS name the certificate is issued for (the address clients type).
	Host string
	// Dir keeps the ACME account key and the current certificate and key (created 0700, files 0600).
	Dir string
	// DirectoryURL is the ACME server; empty means Let's Encrypt production. Staging is
	// https://acme-staging-v02.api.letsencrypt.org/directory (trusted by nobody: for trying the flow).
	DirectoryURL string
	// HTTPAddr is where http-01 validation is answered (the CA connects to port 80 of Host): ":80" by default.
	HTTPAddr string
	// Log receives progress and failures.
	Log *slog.Logger
}

type Manager struct {
	cfg  Config
	cert atomic.Pointer[tls.Certificate]
	leaf atomic.Pointer[x509.Certificate]
	tok  sync.Map // http-01 token -> key authorization
	busy sync.Mutex
	now  func() time.Time
}

// New loads the certificate kept in cfg.Dir, if any. It does not touch the network.
func New(cfg Config) (*Manager, error) {
	if cfg.Host == "" || cfg.Dir == "" {
		return nil, errors.New("tlscert: Host and Dir are required")
	}
	if cfg.DirectoryURL == "" {
		cfg.DirectoryURL = LetsEncryptProduction
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":80"
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", cfg.Dir, err)
	}
	m := &Manager{cfg: cfg, now: time.Now}
	if err := m.load(); err != nil {
		cfg.Log.Warn("no usable certificate on disk yet", "error", err)
	}
	return m, nil
}

func (m *Manager) certPath() string { return filepath.Join(m.cfg.Dir, "cert.pem") }
func (m *Manager) keyPath() string  { return filepath.Join(m.cfg.Dir, "key.pem") }

func (m *Manager) load() error {
	pair, err := tls.LoadX509KeyPair(m.certPath(), m.keyPath())
	if err != nil {
		return err
	}
	return m.accept(pair)
}

// accept checks a certificate is for Host and starts serving it.
func (m *Manager) accept(pair tls.Certificate) error {
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	if err := leaf.VerifyHostname(m.cfg.Host); err != nil {
		return fmt.Errorf("the certificate is for another address: %w", err)
	}
	pair.Leaf = leaf
	m.cert.Store(&pair)
	m.leaf.Store(leaf)
	return nil
}

// Ready reports whether a certificate that has not expired is in place.
func (m *Manager) Ready() bool {
	l := m.leaf.Load()
	return l != nil && m.now().Before(l.NotAfter)
}

// TLSConfig is the configuration for the listener: it always serves the current certificate, so a renewal needs no restart.
func (m *Manager) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			c := m.cert.Load()
			if c == nil {
				return nil, errors.New("no certificate yet")
			}
			return c, nil
		},
	}
}

// needsRenewal is true when there is no certificate, or half of its life is gone (the usual rule, and the only sane one for a six-day certificate).
func needsRenewal(leaf *x509.Certificate, now time.Time) bool {
	if leaf == nil {
		return true
	}
	life := leaf.NotAfter.Sub(leaf.NotBefore)
	return !now.Before(leaf.NotBefore.Add(life / 2))
}

// Run answers validation on HTTPAddr and keeps the certificate fresh until ctx ends. A failed attempt is logged and retried the next round; the
// certificate in use keeps serving until it actually expires.
func (m *Manager) Run(ctx context.Context) {
	srv := &http.Server{Addr: m.cfg.HTTPAddr, Handler: m.challengeHandler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	ln, err := net.Listen("tcp", m.cfg.HTTPAddr)
	if err != nil {
		m.cfg.Log.Error("cannot listen for certificate validation: renewal will fail", "addr", m.cfg.HTTPAddr, "error", err)
	} else {
		go func() { _ = srv.Serve(ln) }()
		defer srv.Close()
	}
	round := func() {
		if !needsRenewal(m.leaf.Load(), m.now()) {
			return
		}
		if err := m.obtain(ctx); err != nil {
			m.cfg.Log.Error("getting a certificate failed", "host", m.cfg.Host, "error", err)
		}
	}
	round()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			round()
		}
	}
}

// challengeHandler answers the CA's http-01 request and sends every other visitor on port 80 to https.
func (m *Manager) challengeHandler() http.Handler {
	const prefix = "/.well-known/acme-challenge/"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) > len(prefix) && r.URL.Path[:len(prefix)] == prefix {
			if v, ok := m.tok.Load(r.URL.Path[len(prefix):]); ok {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte(v.(string)))
				return
			}
			http.NotFound(w, r)
			return
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		http.Redirect(w, r, "https://"+host+":"+m.httpsPort()+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
}

// RedirectPort is the port plain visitors on port 80 are sent to (the main HTTP port).
var RedirectPort = "8080"

func (m *Manager) httpsPort() string { return RedirectPort }

func (m *Manager) accountKey() (*ecdsa.PrivateKey, error) {
	path := filepath.Join(m.cfg.Dir, "account.key")
	if b, err := os.ReadFile(path); err == nil {
		if blk, _ := pem.Decode(b); blk != nil {
			return x509.ParseECPrivateKey(blk.Bytes)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return key, writeFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
}

func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// obtain runs one ACME order for Host and, on success, swaps the new certificate in.
func (m *Manager) obtain(ctx context.Context) error {
	m.busy.Lock()
	defer m.busy.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	accKey, err := m.accountKey()
	if err != nil {
		return fmt.Errorf("account key: %w", err)
	}
	client := &acme.Client{Key: accKey, DirectoryURL: m.cfg.DirectoryURL, UserAgent: "lazycake-coordinator"}
	if _, err := client.Register(ctx, &acme.Account{}, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return fmt.Errorf("registering the ACME account: %w", err)
	}

	id := acme.AuthzID{Type: "dns", Value: m.cfg.Host}
	var opts []acme.OrderOption
	ip := net.ParseIP(m.cfg.Host)
	if ip != nil {
		id.Type = "ip"
		opts = append(opts, acme.WithOrderIssuanceProfile("shortlived")) // the only profile that allows IP addresses
	}
	m.cfg.Log.Info("requesting a certificate", "host", m.cfg.Host, "type", id.Type, "directory", m.cfg.DirectoryURL)
	order, err := client.AuthorizeOrder(ctx, []acme.AuthzID{id}, opts...)
	if err != nil {
		return fmt.Errorf("creating the order: %w", err)
	}
	for _, u := range order.AuthzURLs {
		if err := m.validate(ctx, client, u); err != nil {
			return err
		}
	}
	if _, err := client.WaitOrder(ctx, order.URI); err != nil {
		return fmt.Errorf("waiting for the order: %w", err)
	}

	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := &x509.CertificateRequest{}
	if ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{m.cfg.Host}
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, tmpl, certKey)
	if err != nil {
		return err
	}
	der, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return fmt.Errorf("finalizing the order: %w", err)
	}
	return m.install(der, certKey)
}

func (m *Manager) validate(ctx context.Context, client *acme.Client, authzURL string) error {
	authz, err := client.GetAuthorization(ctx, authzURL)
	if err != nil {
		return fmt.Errorf("reading the authorization: %w", err)
	}
	if authz.Status == acme.StatusValid {
		return nil
	}
	var chal *acme.Challenge
	for _, c := range authz.Challenges {
		if c.Type == "http-01" {
			chal = c
		}
	}
	if chal == nil {
		return errors.New("the CA offered no http-01 challenge")
	}
	keyAuth, err := client.HTTP01ChallengeResponse(chal.Token)
	if err != nil {
		return err
	}
	m.tok.Store(chal.Token, keyAuth)
	defer m.tok.Delete(chal.Token)
	if _, err := client.Accept(ctx, chal); err != nil {
		return fmt.Errorf("accepting the challenge: %w", err)
	}
	if _, err := client.WaitAuthorization(ctx, authzURL); err != nil {
		return fmt.Errorf("the CA could not validate %s over http (is port 80 reachable from the internet?): %w", m.cfg.Host, err)
	}
	return nil
}

func (m *Manager) install(der [][]byte, key *ecdsa.PrivateKey) error {
	var chain []byte
	for _, d := range der {
		chain = append(chain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: d})...)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	// Check it before touching the files: a bad certificate must never replace a good one.
	pair, err := tls.X509KeyPair(chain, keyPEM)
	if err != nil {
		return fmt.Errorf("the new certificate does not load: %w", err)
	}
	if err := m.accept(pair); err != nil {
		return fmt.Errorf("the new certificate is not usable: %w", err)
	}
	if err := writeFile(m.keyPath(), keyPEM); err != nil {
		return err
	}
	if err := writeFile(m.certPath(), chain); err != nil {
		return err
	}
	l := m.leaf.Load()
	m.cfg.Log.Info("certificate installed", "host", m.cfg.Host, "valid_from", l.NotBefore, "valid_until", l.NotAfter)
	return nil
}
