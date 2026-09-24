package noise

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"testing"
)

// pipe returns two connected in-memory net.Conn ends, standing in for the
// outer QUIC/TCP transport this package is carried inside.
func pipe() (net.Conn, net.Conn) {
	return net.Pipe()
}

func handshakePair(t *testing.T) (initiator, responder *Session) {
	t.Helper()
	initKP, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	respKP, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}

	a, b := pipe()
	t.Cleanup(func() { a.Close(); b.Close() })

	type result struct {
		s   *Session
		rs  []byte
		err error
	}
	initCh := make(chan result, 1)
	respCh := make(chan result, 1)

	go func() {
		s, err := DoInitiatorHandshake(a, initKP, respKP.Public)
		initCh <- result{s: s, err: err}
	}()
	go func() {
		s, rs, err := DoResponderHandshake(b, respKP)
		respCh <- result{s: s, rs: rs, err: err}
	}()

	ir := <-initCh
	rr := <-respCh
	if ir.err != nil {
		t.Fatalf("initiator handshake: %v", ir.err)
	}
	if rr.err != nil {
		t.Fatalf("responder handshake: %v", rr.err)
	}
	if !bytes.Equal(rr.rs, initKP.Public) {
		t.Fatalf("responder learned wrong initiator static key")
	}
	return ir.s, rr.s
}

func TestPublicFromPrivateMatchesGenerated(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := PublicFromPrivate(kp.Private)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pub, kp.Public) {
		t.Fatal("derived public key does not match the one GenerateKeypair returned")
	}
}

func TestRoundTrip10MB(t *testing.T) {
	initiator, responder := handshakePair(t)

	payload := make([]byte, 10<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := initiator.Write(payload)
		errCh <- err
	}()

	got := make([]byte, len(payload))
	if _, err := io.ReadFull(responder, got); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("writing: %v", err)
	}

	if !bytes.Equal(payload, got) {
		t.Fatal("round-tripped bytes do not match")
	}
}

func TestTamperedCiphertextFailsAuthentication(t *testing.T) {
	initiator, responder := handshakePair(t)

	// Route the initiator's frame through an in-memory buffer instead of
	// the live pipe, so a byte inside it can be flipped before decryption.
	var buf bytes.Buffer
	initiator.rw = &buf
	if _, err := initiator.Write([]byte("hello")); err != nil {
		t.Fatalf("writing: %v", err)
	}

	tampered := append([]byte(nil), buf.Bytes()...)
	tampered[len(tampered)-1] ^= 0xFF // flip the last byte (inside the AEAD tag)

	responder.rw = bytes.NewBuffer(tampered)
	out := make([]byte, 16)
	if _, err := responder.Read(out); err == nil {
		t.Fatal("expected authentication failure on tampered ciphertext")
	}
}
