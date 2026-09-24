package netns

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/mkassab215/lazycake/internal/tunnel/noise"
)

// Config is everything both halves of a proxy need. It is JSON-
// serializable so it can be handed to the re-exec'd bind-only child
// process over stdin (see ServeSubcommand): setns(CLONE_NEWUSER) is
// documented to fail with EINVAL from a multithreaded caller, and every Go
// process is multithreaded, so joining the container's namespaces has to
// happen in a fresh single-threaded process - nsenter itself - before any
// of this code runs, rather than via a direct syscall from an already-
// running agent.
//
// The child that inherits the joined namespaces only binds sockets and
// hands their file descriptors back to the parent (see sendFDs/recvFDs):
// a --network=none container's namespace has no route out to the relay at
// all, so accepting connections and dialing the relay has to happen in the
// parent, which has the host's normal networking. A bound listening
// socket keeps working from any namespace once handed over - only the
// bind() call itself is namespace-sensitive.
type Config struct {
	TaskID       string
	AgentKeypair noise.Keypair
	Targets      []Target
}

// bindAll binds the stub resolver and one TCP listener per target,
// assigning addresses along the way. Assumes the calling process is
// already inside the target network namespace.
func bindAll(cfg *Config) (*net.UDPConn, []*net.TCPListener, error) {
	targets, err := AssignAddresses(cfg.Targets)
	if err != nil {
		return nil, nil, err
	}
	cfg.Targets = targets

	dnsConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 53), Port: 53})
	if err != nil {
		return nil, nil, fmt.Errorf("binding stub resolver: %w", err)
	}

	listeners := make([]*net.TCPListener, len(targets))
	for i, t := range targets {
		ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: t.Addr, Port: int(t.Port)})
		if err != nil {
			dnsConn.Close()
			for _, l := range listeners[:i] {
				l.Close()
			}
			return nil, nil, fmt.Errorf("binding listener for %s: %w", t.Hostname, err)
		}
		listeners[i] = ln
	}
	return dnsConn, listeners, nil
}

// ServeSubcommand implements `agent __netns_proxy`: reads a Config as JSON
// from stdin, binds every socket (already inside the target namespace by
// exec inheritance from nsenter - see Proxy.Setup), sends their file
// descriptors to the parent over fd 3, writes the (address-assigned)
// Config back as JSON on fd 4 so the parent knows which target maps to
// which listener (by index), and exits. It does no serving itself - see
// the package doc comment on Config for why.
func ServeSubcommand() error {
	var cfg Config
	if err := json.NewDecoder(os.Stdin).Decode(&cfg); err != nil {
		return fmt.Errorf("decoding proxy config: %w", err)
	}

	dnsConn, listeners, err := bindAll(&cfg)
	if err != nil {
		return err
	}

	ctlConn, err := net.FileConn(os.NewFile(3, "ctl"))
	if err != nil {
		return fmt.Errorf("wrapping control fd: %w", err)
	}
	unixCtl, ok := ctlConn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("control fd is not a unix socket")
	}

	files := make([]*os.File, 0, len(listeners)+1)
	dnsFile, err := dnsConn.File()
	if err != nil {
		return fmt.Errorf("getting dns conn file: %w", err)
	}
	files = append(files, dnsFile)
	for _, ln := range listeners {
		f, err := ln.File()
		if err != nil {
			return fmt.Errorf("getting listener file: %w", err)
		}
		files = append(files, f)
	}

	if err := json.NewEncoder(os.NewFile(4, "result")).Encode(cfg); err != nil {
		return fmt.Errorf("writing result config: %w", err)
	}

	return sendFDs(unixCtl, files)
}
