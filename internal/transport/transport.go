// Package transport decides how the agent, the gateway and lcctl secure their connections to the coordinator (gRPC on 7443 and the QUIC tunnel
// relay on 7444). Both carry bearer tokens, so on any real network they must be TLS with the certificate verified.
//
// The rule is the same everywhere: TLS is on for every coordinator address that is not on this machine, and off for loopback (local development
// and tests). LAZYCAKE_COORDINATOR_TLS=0 forces plaintext, =1 forces TLS. There is deliberately no "try TLS, fall back to plaintext": a fallback
// is what lets an attacker who can drop packets downgrade a connection and read the token.
package transport

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"strings"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// RootCAs, when set, replaces the system certificate pool (tests, or a private CA). nil means the system pool.
var RootCAs *x509.CertPool

// Enabled reports whether connections to addr ("host:port") use verified TLS.
func Enabled(addr string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LAZYCAKE_COORDINATOR_TLS"))) {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	}
	return !isLocal(addr)
}

// isLocal is true for an address on this machine: loopback, the unspecified addresses ("0.0.0.0", "::", which mean "here" when dialled), and ":port".
func isLocal(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func host(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// GRPCCredentials is what to pass to grpc.WithTransportCredentials for the connection to addr.
func GRPCCredentials(addr string) credentials.TransportCredentials {
	if !Enabled(addr) {
		return insecure.NewCredentials()
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: RootCAs})
}

// QUICConfig is the client TLS configuration for the relay at addr: the certificate is verified (against the address in addr) unless TLS is off
// for it, in which case verification is skipped as it always was (the payload is Noise-encrypted end to end either way).
func QUICConfig(addr string, nextProtos []string) *tls.Config {
	c := &tls.Config{NextProtos: nextProtos, MinVersion: tls.VersionTLS13}
	if Enabled(addr) {
		c.ServerName = host(addr)
		c.RootCAs = RootCAs
	} else {
		c.InsecureSkipVerify = true
	}
	return c
}
