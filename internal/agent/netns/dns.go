package netns

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
)

// A minimal, hand-rolled DNS server: this only ever needs to answer A
// queries for a fixed, tiny set of hostnames with a fixed answer, so a
// full resolver library would be pulling in far more than the job needs.
// Anything it doesn't recognise gets NXDOMAIN - there is nothing else to
// resolve inside this netns, by design.

const (
	dnsHeaderLen  = 12
	qtypeA        = 1
	qtypeAAAA     = 28
	qclassIN      = 1
	rcodeNXDomain = 3
)

// parseQuestionName reads the QNAME starting at offset dnsHeaderLen and
// returns it dot-joined (without a trailing dot), plus the byte offset
// immediately after QTYPE+QCLASS, plus the raw QTYPE.
func parseQuestionName(msg []byte) (name string, qtype uint16, end int, err error) {
	if len(msg) < dnsHeaderLen+1 {
		return "", 0, 0, fmt.Errorf("message too short")
	}
	var labels []string
	i := dnsHeaderLen
	for {
		if i >= len(msg) {
			return "", 0, 0, fmt.Errorf("truncated qname")
		}
		l := int(msg[i])
		if l == 0 {
			i++
			break
		}
		if l&0xC0 != 0 {
			return "", 0, 0, fmt.Errorf("compressed qname not supported in a query")
		}
		i++
		if i+l > len(msg) {
			return "", 0, 0, fmt.Errorf("truncated label")
		}
		labels = append(labels, string(msg[i:i+l]))
		i += l
	}
	if i+4 > len(msg) {
		return "", 0, 0, fmt.Errorf("truncated qtype/qclass")
	}
	qtype = binary.BigEndian.Uint16(msg[i : i+2])
	return strings.Join(labels, "."), qtype, i + 4, nil
}

// buildAnswer builds a full DNS response. Three cases, deliberately kept
// distinct even though a naive implementation might collapse "no A
// record" and "name unknown" into the same NXDOMAIN: RFC 1035 reserves
// NXDOMAIN for "this name doesn't exist at all", and getting that wrong
// for a known-name/wrong-type query (e.g. AAAA for a name that only has
// an A record) trips up real resolvers - musl (Alpine's libc, so every
// container this actually runs against) gives up on the whole lookup,
// A record included, if an earlier AAAA query for the same name came
// back NXDOMAIN rather than a proper empty NOERROR.
//   - nxdomain: name isn't one of this task's targets at all.
//   - !nxdomain, !found: name is known but not for the queried type
//     (e.g. AAAA) - NOERROR with an empty answer section.
//   - found: NOERROR with one A record.
func buildAnswer(msg []byte, questionEnd int, found, nxdomain bool, ip net.IP) []byte {
	id := msg[0:2]
	out := make([]byte, 0, questionEnd+16)
	out = append(out, id...)

	flags := uint16(0x8180) // QR=1, RD=1, RA=1
	if nxdomain {
		flags |= rcodeNXDomain
	}
	var flagsBuf [2]byte
	binary.BigEndian.PutUint16(flagsBuf[:], flags)
	out = append(out, flagsBuf[:]...)

	out = append(out, 0, 1) // QDCOUNT=1
	if found {
		out = append(out, 0, 1) // ANCOUNT=1
	} else {
		out = append(out, 0, 0)
	}
	out = append(out, 0, 0) // NSCOUNT=0
	out = append(out, 0, 0) // ARCOUNT=0

	out = append(out, msg[dnsHeaderLen:questionEnd]...) // echo the question

	if found {
		out = append(out, 0xC0, 0x0C) // name = pointer to question's QNAME
		out = append(out, 0, qtypeA)
		out = append(out, 0, qclassIN)
		out = append(out, 0, 0, 0, 5) // TTL = 5s: short, since these mappings are per-task
		out = append(out, 0, 4)       // RDLENGTH = 4
		out = append(out, ip.To4()...)
	}
	return out
}

// resolve answers name against targets, case-insensitively.
func resolve(targets []Target, name string) (net.IP, bool) {
	for _, t := range targets {
		if strings.EqualFold(t.Hostname, name) {
			return t.Addr, true
		}
	}
	return nil, false
}

// handleDNSQuery is the pure core of the stub resolver: parse, look up,
// respond. AAAA (and anything else non-A) always comes back empty -
// NOERROR if the name is one of this task's targets, NXDOMAIN otherwise -
// so this proxy only ever hands out the IPv4 addresses it assigned, but
// without lying about whether the name itself exists (see buildAnswer).
func handleDNSQuery(targets []Target, query []byte) ([]byte, error) {
	name, qtype, end, err := parseQuestionName(query)
	if err != nil {
		return nil, fmt.Errorf("parsing query: %w", err)
	}
	ip, known := resolve(targets, name)
	if !known {
		return buildAnswer(query, end, false, true, nil), nil
	}
	if qtype != qtypeA {
		return buildAnswer(query, end, false, false, nil), nil
	}
	return buildAnswer(query, end, true, false, ip), nil
}
