package netns

import (
	"encoding/binary"
	"net"
	"testing"
)

// buildQuery hand-encodes a minimal DNS query for name/qtype, for testing.
func buildQuery(id uint16, name string, qtype uint16) []byte {
	msg := make([]byte, 2, 32)
	binary.BigEndian.PutUint16(msg, id)
	msg = append(msg, 0x01, 0x00) // flags: RD=1
	msg = append(msg, 0, 1)       // QDCOUNT=1
	msg = append(msg, 0, 0, 0, 0, 0, 0)

	for _, label := range splitLabels(name) {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0)

	var qt [2]byte
	binary.BigEndian.PutUint16(qt[:], qtype)
	msg = append(msg, qt[:]...)
	msg = append(msg, 0, qclassIN)
	return msg
}

func splitLabels(name string) []string {
	var labels []string
	start := 0
	for i, c := range name {
		if c == '.' {
			labels = append(labels, name[start:i])
			start = i + 1
		}
	}
	labels = append(labels, name[start:])
	return labels
}

func TestParseQuestionName(t *testing.T) {
	q := buildQuery(1234, "db.acme.com", qtypeA)
	name, qtype, _, err := parseQuestionName(q)
	if err != nil {
		t.Fatal(err)
	}
	if name != "db.acme.com" {
		t.Fatalf("got name %q", name)
	}
	if qtype != qtypeA {
		t.Fatalf("got qtype %d", qtype)
	}
}

func TestHandleDNSQueryMatch(t *testing.T) {
	targets := []Target{{Hostname: "db.acme.com", Addr: net.IPv4(127, 0, 10, 2)}}
	q := buildQuery(42, "db.acme.com", qtypeA)

	resp, err := handleDNSQuery(targets, q)
	if err != nil {
		t.Fatal(err)
	}

	if binary.BigEndian.Uint16(resp[0:2]) != 42 {
		t.Fatal("response ID does not match query ID")
	}
	ancount := binary.BigEndian.Uint16(resp[6:8])
	if ancount != 1 {
		t.Fatalf("expected ANCOUNT=1, got %d", ancount)
	}
	// last 4 bytes of a single-answer response are the A record's RDATA.
	gotIP := net.IP(resp[len(resp)-4:])
	if !gotIP.Equal(net.IPv4(127, 0, 10, 2)) {
		t.Fatalf("got IP %v, want 127.0.10.2", gotIP)
	}
}

func TestHandleDNSQueryNoMatchIsNXDomain(t *testing.T) {
	targets := []Target{{Hostname: "db.acme.com", Addr: net.IPv4(127, 0, 10, 2)}}
	q := buildQuery(1, "evil.example.com", qtypeA)

	resp, err := handleDNSQuery(targets, q)
	if err != nil {
		t.Fatal(err)
	}
	flags := binary.BigEndian.Uint16(resp[2:4])
	rcode := flags & 0x0F
	if rcode != rcodeNXDomain {
		t.Fatalf("expected NXDOMAIN, got rcode %d", rcode)
	}
	ancount := binary.BigEndian.Uint16(resp[6:8])
	if ancount != 0 {
		t.Fatalf("expected ANCOUNT=0 for NXDOMAIN, got %d", ancount)
	}
}

// A known hostname's AAAA query must come back NOERROR-but-empty, not
// NXDOMAIN: musl (Alpine's libc, so every real container this runs
// against) treats an NXDOMAIN on any query type for a name as the whole
// name not existing, and gives up on a getaddrinfo() call entirely - even
// after a separate A query for the same name already succeeded.
func TestHandleDNSQueryAAAAKnownHostnameIsEmptyNotNXDomain(t *testing.T) {
	targets := []Target{{Hostname: "db.acme.com", Addr: net.IPv4(127, 0, 10, 2)}}
	q := buildQuery(1, "db.acme.com", qtypeAAAA)

	resp, err := handleDNSQuery(targets, q)
	if err != nil {
		t.Fatal(err)
	}
	flags := binary.BigEndian.Uint16(resp[2:4])
	if flags&0x0F != 0 {
		t.Fatalf("expected NOERROR (rcode 0) for a known hostname's AAAA query, got rcode %d", flags&0x0F)
	}
	ancount := binary.BigEndian.Uint16(resp[6:8])
	if ancount != 0 {
		t.Fatalf("expected ANCOUNT=0 (no AAAA record to give), got %d", ancount)
	}
}

func TestHandleDNSQueryAAAAUnknownHostnameIsNXDomain(t *testing.T) {
	targets := []Target{{Hostname: "db.acme.com", Addr: net.IPv4(127, 0, 10, 2)}}
	q := buildQuery(1, "evil.example.com", qtypeAAAA)

	resp, err := handleDNSQuery(targets, q)
	if err != nil {
		t.Fatal(err)
	}
	flags := binary.BigEndian.Uint16(resp[2:4])
	if flags&0x0F != rcodeNXDomain {
		t.Fatal("expected an unknown hostname's AAAA query to still get NXDOMAIN")
	}
}

func TestAssignAddressesDistinct(t *testing.T) {
	targets := []Target{{Hostname: "a"}, {Hostname: "b"}, {Hostname: "c"}}
	got, err := AssignAddresses(targets)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tg := range got {
		if tg.Addr == nil {
			t.Fatal("expected every target to get an address")
		}
		s := tg.Addr.String()
		if seen[s] {
			t.Fatalf("duplicate address assigned: %s", s)
		}
		seen[s] = true
		if !tg.Addr.IsLoopback() {
			t.Fatalf("expected a loopback address, got %s", s)
		}
	}
}

func TestServiceName(t *testing.T) {
	cases := map[string]string{
		"db.acme.com": "db",
		"cache":       "cache",
	}
	for in, want := range cases {
		if got := serviceName(in); got != want {
			t.Errorf("serviceName(%q) = %q, want %q", in, got, want)
		}
	}
}
