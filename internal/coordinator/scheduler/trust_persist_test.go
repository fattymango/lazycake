//go:build integration

package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fattymango/lazycake/internal/coordinator/store"
)

// A restart of the coordinator must not forget what each machine has earned, or lost: scores were written to the database but never read back.
func TestTrustSurvivesARestart(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_1", Name: "act_1"}))
	for _, id := range []string{"nod_good", "nod_banned", "nod_new"} {
		require.NoError(t, st.UpsertNode(ctx, store.Node{ID: id, AccountID: "act_1", Hostname: id, Arch: "amd64"}))
	}

	before := NewTrustTracker()
	before.Store = st
	before.Log = discardLog()
	for i := 0; i < 30; i++ {
		before.CleanCompletion("nod_good") // 0.5 + 0.30
	}
	for i := 0; i < 3; i++ {
		before.CanaryFailed("nod_banned") // down to the floor
	}
	goodScore, bannedScore := before.Score("nod_good"), before.Score("nod_banned")
	require.Greater(t, goodScore, 0.75)
	require.True(t, before.Banned("nod_banned"))

	// "Restart": a brand new tracker that knows nothing but the database.
	after := NewTrustTracker()
	require.NoError(t, after.LoadFrom(ctx, st))
	require.InDelta(t, goodScore, after.Score("nod_good"), 0.0011, "an honest machine keeps what it earned")
	require.InDelta(t, bannedScore, after.Score("nod_banned"), 0.0011)
	require.True(t, after.Banned("nod_banned"), "a banned machine does not get a clean slate by the coordinator restarting")
	require.Equal(t, TrustStart, after.Score("nod_new"), "a machine nobody has scored starts at the default")
	require.Equal(t, TrustStart, after.Score("nod_unknown"))
}
