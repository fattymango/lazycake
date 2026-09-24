// Package netns is the agent-side half of the tunnel described in
// PLAN.md "Networking": for a task's container, run entirely with
// --network=none plus a proxy this package sets up *inside* that
// container's own network namespace - a stub DNS resolver that only
// answers the task's declared target hostnames, each mapped to its own
// loopback address, and a TCP listener per target that dials out through
// the QUIC+Noise tunnel to the right gateway. Unlisted hosts have no
// route and don't resolve; that's what makes the container's network
// isolation hold "by construction, not by policy" (PLAN.md).
package netns

import (
	"fmt"
	"net"
)

// Target is one gateway-reachable hostname a task is allowed to resolve
// and connect to, from lazycakev1.TunnelTarget.
type Target struct {
	GatewayID   string
	Hostname    string
	Port        int32
	NoisePubkey []byte

	// Addr is filled in by AssignAddresses: the loopback address this
	// hostname resolves to inside the container's netns.
	Addr net.IP
}

// addressBase is the /24 PLAN.md's stub resolver hands out addresses
// from. .0 and .1 are left unused (network address and a conventional
// "gateway" address some tools assume exists) so assignment starts at .2.
var addressBase = net.IPv4(127, 0, 10, 0).To4()

// maxTargets is enforced by addressBase being a /24: 253 usable host
// addresses (.2-.254). PLAN.md's own cap of 3 gateways per task is far
// below this; it exists so a single misbehaving task can't exhaust it.
const maxTargets = 253

// AssignAddresses gives each target a distinct address from 127.0.10.0/24
// and returns the same slice with Addr filled in.
func AssignAddresses(targets []Target) ([]Target, error) {
	if len(targets) > maxTargets {
		return nil, fmt.Errorf("netns: %d targets exceeds the %d this proxy can address", len(targets), maxTargets)
	}
	for i := range targets {
		ip := make(net.IP, 4)
		copy(ip, addressBase)
		ip[3] = byte(i + 2)
		targets[i].Addr = ip
	}
	return targets, nil
}

// serviceName is the wire convention with the gateway (see
// internal/gateway/listener): the first label of the hostname, e.g.
// "db" from "db.acme.com". Gateways publish services under this same
// short name.
func serviceName(hostname string) string {
	for i, c := range hostname {
		if c == '.' {
			return hostname[:i]
		}
	}
	return hostname
}
