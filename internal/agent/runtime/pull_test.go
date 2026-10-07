package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	dockerclient "github.com/docker/docker/client"
)

// fakeEngine answers just enough of the Docker API for Pull: ping, image inspect, and image create (the pull).
func fakeEngine(t *testing.T, haveImage bool) (*PodmanRuntime, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var pulls, inspects atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.41")
			_, _ = w.Write([]byte("OK"))
		case strings.Contains(r.URL.Path, "/images/create"):
			pulls.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"pulled"}`))
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			inspects.Add(1)
			if !haveImage && pulls.Load() == 0 {
				http.Error(w, `{"message":"no such image"}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"sha256:abc","Size":8104044}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cli, err := dockerclient.NewClientWithOpts(dockerclient.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")), dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	return &PodmanRuntime{cli: cli}, &pulls, &inspects
}

const pinned = "docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e"

// The load-test finding: a cached, digest-pinned image must not touch the registry.
func TestPullSkipsTheRegistryForACachedDigestPinnedImage(t *testing.T) {
	rt, pulls, _ := fakeEngine(t, true)
	size, err := rt.Pull(context.Background(), pinned)
	if err != nil || size != 8104044 {
		t.Fatalf("size %d err %v", size, err)
	}
	if pulls.Load() != 0 {
		t.Fatalf("a cached digest-pinned image was pulled anyway (%d registry round trips)", pulls.Load())
	}
}

func TestPullFetchesAnImageThatIsNotCached(t *testing.T) {
	rt, pulls, _ := fakeEngine(t, false)
	if _, err := rt.Pull(context.Background(), pinned); err != nil {
		t.Fatal(err)
	}
	if pulls.Load() != 1 {
		t.Fatalf("an image that isn't cached must be pulled exactly once, got %d", pulls.Load())
	}
}

// A tag can move, so a cached tag proves nothing: only digest-pinned references skip the registry.
func TestPullStillChecksTheRegistryForAMovableTag(t *testing.T) {
	rt, pulls, _ := fakeEngine(t, true)
	if _, err := rt.Pull(context.Background(), "docker.io/library/alpine:latest"); err != nil {
		t.Fatal(err)
	}
	if pulls.Load() != 1 {
		t.Fatalf("a tag must still be pulled to see whether it moved, got %d pulls", pulls.Load())
	}
}

func TestIsDigestPinned(t *testing.T) {
	for ref, want := range map[string]bool{pinned: true, "alpine:latest": false, "alpine": false, "repo@sha256:abc": true, "repo@sha512:abc": false} {
		if got := isDigestPinned(ref); got != want {
			t.Errorf("isDigestPinned(%q) = %v, want %v", ref, got, want)
		}
	}
}
