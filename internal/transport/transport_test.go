package transport

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTLSIsOnForRemoteAddressesAndOffForThisMachine(t *testing.T) {
	t.Setenv("LAZYCAKE_COORDINATOR_TLS", "")
	for addr, want := range map[string]bool{
		"68.183.72.125:7443": true, "coordinator.example.com:7443": true, "192.168.1.10:7443": true,
		"127.0.0.1:7443": false, "localhost:7443": false, "[::1]:7443": false, ":7443": false, "[::]:7443": false, "0.0.0.0:7443": false,
	} {
		require.Equal(t, want, Enabled(addr), addr)
	}
}

func TestEnvOverridesTheRuleBothWays(t *testing.T) {
	t.Setenv("LAZYCAKE_COORDINATOR_TLS", "0")
	require.False(t, Enabled("68.183.72.125:7443"), "forced off (a coordinator without TLS on a LAN)")
	t.Setenv("LAZYCAKE_COORDINATOR_TLS", "1")
	require.True(t, Enabled("127.0.0.1:7443"), "forced on")
}

func TestQUICConfigVerifiesUnlessOff(t *testing.T) {
	t.Setenv("LAZYCAKE_COORDINATOR_TLS", "")
	c := QUICConfig("68.183.72.125:7444", []string{"lazycake-tunnel"})
	require.False(t, c.InsecureSkipVerify)
	require.Equal(t, "68.183.72.125", c.ServerName)
	require.Equal(t, []string{"lazycake-tunnel"}, c.NextProtos)
	require.True(t, QUICConfig("127.0.0.1:7444", nil).InsecureSkipVerify, "loopback dev server: as before")
}
