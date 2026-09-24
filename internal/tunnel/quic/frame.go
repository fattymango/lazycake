package quic

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Every frame this package sends is one or more length-prefixed strings.
// Deliberately not protobuf: this is transport-plumbing metadata (who is
// this stream for), not the application protocol, and keeping it tiny and
// dependency-free keeps the relay's job - "pump bytes, never decode
// payload" - visibly separate from anything that looks like it might peek
// into task data.

func writeString(w io.Writer, s string) error {
	if len(s) > 0xFFFF {
		return fmt.Errorf("string too long: %d bytes", len(s))
	}
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(s)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

func readString(r io.Reader) (string, error) {
	var lenBuf [2]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return "", err
	}
	n := binary.BigEndian.Uint16(lenBuf[:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// controlFrame is the first thing a connection sends, once, identifying
// who's connecting and with what.
type controlFrame struct {
	Role        string // "agent" | "gateway"
	Token       string
	GatewayID   string // only meaningful for role == "gateway"
	NoisePubkey string // hex-encoded; only meaningful for role == "gateway"
}

func writeControlFrame(w io.Writer, f controlFrame) error {
	if err := writeString(w, f.Role); err != nil {
		return err
	}
	if err := writeString(w, f.Token); err != nil {
		return err
	}
	if err := writeString(w, f.GatewayID); err != nil {
		return err
	}
	return writeString(w, f.NoisePubkey)
}

func readControlFrame(r io.Reader) (controlFrame, error) {
	var f controlFrame
	var err error
	if f.Role, err = readString(r); err != nil {
		return f, err
	}
	if f.Token, err = readString(r); err != nil {
		return f, err
	}
	if f.GatewayID, err = readString(r); err != nil {
		return f, err
	}
	if f.NoisePubkey, err = readString(r); err != nil {
		return f, err
	}
	return f, nil
}

// streamHeader is the first thing on every relayed stream the agent opens,
// telling the relay which gateway to forward it to and which task it's
// for (for accounting/logging - the relay still never looks at anything
// after this header).
type streamHeader struct {
	GatewayID string
	TaskID    string
}

func writeStreamHeader(w io.Writer, h streamHeader) error {
	if err := writeString(w, h.GatewayID); err != nil {
		return err
	}
	return writeString(w, h.TaskID)
}

func readStreamHeader(r io.Reader) (streamHeader, error) {
	var h streamHeader
	var err error
	if h.GatewayID, err = readString(r); err != nil {
		return h, err
	}
	if h.TaskID, err = readString(r); err != nil {
		return h, err
	}
	return h, nil
}

// gatewayStreamHeader is what the relay sends on the stream it opens
// toward the gateway, so the gateway knows which task this connection is
// for without needing to know anything about the agent side.
type gatewayStreamHeader struct {
	TaskID string
}

func writeGatewayStreamHeader(w io.Writer, h gatewayStreamHeader) error {
	return writeString(w, h.TaskID)
}

func readGatewayStreamHeader(r io.Reader) (gatewayStreamHeader, error) {
	var h gatewayStreamHeader
	var err error
	h.TaskID, err = readString(r)
	return h, err
}
