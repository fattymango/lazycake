// Package noise implements the inner confidentiality layer described in
// PLAN.md "Transport": Noise_IK between the agent's proxy and a gateway,
// carried inside the outer QUIC/TCP transport (package tunnel/quic), which
// only ever sees ciphertext. The Noise private key never leaves the agent
// or the gateway, so the relay - which does terminate QUIC's own TLS -
// cannot read task traffic. See PLAN.md "Confidentiality and threat
// model" for what this layer does and does not protect against (it
// protects from the relay operator, not from the host the agent runs on).
package noise

import (
	"encoding/binary"
	"fmt"
	"io"

	flynnnoise "github.com/flynn/noise"
	"golang.org/x/crypto/curve25519"
)

var cipherSuite = flynnnoise.NewCipherSuite(flynnnoise.DH25519, flynnnoise.CipherChaChaPoly, flynnnoise.HashSHA256)

// Keypair is a Noise static keypair. Public is what gets published at
// gateway registration (PLAN.md "static keys exchanged at gateway
// registration"); Private never leaves the machine that generated it.
type Keypair struct {
	Public  []byte
	Private []byte
}

// GenerateKeypair returns a fresh Curve25519 static keypair.
func GenerateKeypair() (Keypair, error) {
	kp, err := cipherSuite.GenerateKeypair(nil)
	if err != nil {
		return Keypair{}, fmt.Errorf("generating noise keypair: %w", err)
	}
	return Keypair{Public: kp.Public, Private: kp.Private}, nil
}

// PublicFromPrivate derives a Curve25519 public key from a static private
// key, for loading a persisted keypair back from just its private half
// (see cmd/gateway's key persistence).
func PublicFromPrivate(private []byte) ([]byte, error) {
	pub, err := curve25519.X25519(private, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("deriving public key: %w", err)
	}
	return pub, nil
}

// maxPlaintext keeps each Noise message under the protocol's 65535-byte
// ciphertext limit once the AEAD tag is added.
const maxPlaintext = 65535 - 16

// Session is an encrypted, ordered, reliable message stream once the
// handshake completes: Write splits and encrypts, Read decrypts and
// reassembles, so a Session can be used anywhere an io.ReadWriter is
// expected (in particular, wrapped around one QUIC stream in
// internal/tunnel/quic).
type Session struct {
	rw   io.ReadWriter
	send *flynnnoise.CipherState
	recv *flynnnoise.CipherState

	readBuf []byte // leftover decrypted bytes from a Read that returned less than one message
}

// DoInitiatorHandshake performs the IK handshake as the initiator (the
// agent's proxy): it must already know the responder's static public key,
// which is exactly what a TunnelTarget carries from the coordinator.
func DoInitiatorHandshake(rw io.ReadWriter, local Keypair, remoteStaticPub []byte) (*Session, error) {
	hs, err := flynnnoise.NewHandshakeState(flynnnoise.Config{
		CipherSuite:   cipherSuite,
		Pattern:       flynnnoise.HandshakeIK,
		Initiator:     true,
		StaticKeypair: flynnnoise.DHKey{Public: local.Public, Private: local.Private},
		PeerStatic:    remoteStaticPub,
	})
	if err != nil {
		return nil, fmt.Errorf("initializing handshake: %w", err)
	}

	// -> e, es, s, ss
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("writing handshake message 1: %w", err)
	}
	if err := writeFrame(rw, msg1); err != nil {
		return nil, fmt.Errorf("sending handshake message 1: %w", err)
	}

	// <- e, ee, se
	msg2, err := readFrame(rw)
	if err != nil {
		return nil, fmt.Errorf("receiving handshake message 2: %w", err)
	}
	// split() returns (c1, c2): c1 encrypts initiator->responder traffic,
	// c2 the reverse - so as the initiator, send=c1, recv=c2.
	_, c1, c2, err := hs.ReadMessage(nil, msg2)
	if err != nil {
		return nil, fmt.Errorf("reading handshake message 2: %w", err)
	}

	return &Session{rw: rw, send: c1, recv: c2}, nil
}

// DoResponderHandshake performs the IK handshake as the responder (a
// gateway): it does not need to know the initiator's static key in
// advance, and returns it once the handshake completes.
func DoResponderHandshake(rw io.ReadWriter, local Keypair) (*Session, []byte, error) {
	hs, err := flynnnoise.NewHandshakeState(flynnnoise.Config{
		CipherSuite:   cipherSuite,
		Pattern:       flynnnoise.HandshakeIK,
		Initiator:     false,
		StaticKeypair: flynnnoise.DHKey{Public: local.Public, Private: local.Private},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("initializing handshake: %w", err)
	}

	msg1, err := readFrame(rw)
	if err != nil {
		return nil, nil, fmt.Errorf("receiving handshake message 1: %w", err)
	}
	_, _, _, err = hs.ReadMessage(nil, msg1)
	if err != nil {
		return nil, nil, fmt.Errorf("reading handshake message 1: %w", err)
	}
	remoteStatic := hs.PeerStatic()

	// Per the Noise spec, split() always returns (c1, c2) where c1
	// encrypts initiator->responder traffic and c2 encrypts the reverse,
	// regardless of which side is asking - so the responder's send/recv
	// assignment is the mirror image of the initiator's.
	msg2, c1, c2, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("writing handshake message 2: %w", err)
	}
	if err := writeFrame(rw, msg2); err != nil {
		return nil, nil, fmt.Errorf("sending handshake message 2: %w", err)
	}

	return &Session{rw: rw, send: c2, recv: c1}, remoteStatic, nil
}

// Write encrypts p (splitting across multiple Noise messages if needed)
// and sends each as a length-prefixed frame.
func (s *Session) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > maxPlaintext {
			chunk = chunk[:maxPlaintext]
		}
		ciphertext, err := s.send.Encrypt(nil, nil, chunk)
		if err != nil {
			return total, fmt.Errorf("encrypting: %w", err)
		}
		if err := writeFrame(s.rw, ciphertext); err != nil {
			return total, fmt.Errorf("writing frame: %w", err)
		}
		total += len(chunk)
		p = p[len(chunk):]
	}
	return total, nil
}

// Read decrypts and returns application bytes, buffering any leftover
// plaintext from a message larger than the caller's buffer.
func (s *Session) Read(p []byte) (int, error) {
	if len(s.readBuf) == 0 {
		frame, err := readFrame(s.rw)
		if err != nil {
			return 0, err
		}
		plaintext, err := s.recv.Decrypt(nil, nil, frame)
		if err != nil {
			return 0, fmt.Errorf("decrypting: %w", err)
		}
		s.readBuf = plaintext
	}
	n := copy(p, s.readBuf)
	s.readBuf = s.readBuf[n:]
	return n, nil
}

func writeFrame(w io.Writer, data []byte) error {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

func readFrame(r io.Reader) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
