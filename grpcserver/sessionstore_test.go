package grpcserver

import (
	"testing"
	"time"

	"github.com/odvcencio/arbiter/expert"
)

func TestSessionStorePrunesExpiredSessionsOnCreate(t *testing.T) {
	store := NewSessionStore()
	store.ttl = time.Minute

	first := store.Create("bundle_a", nil, &expert.Session{})
	first.LastAccess = time.Now().UTC().Add(-2 * time.Minute)

	second := store.Create("bundle_b", nil, &expert.Session{})
	if second == nil {
		t.Fatal("expected second session")
	}
	if _, ok := store.Get(first.ID); ok {
		t.Fatalf("expected expired session %q to be pruned", first.ID)
	}
	if _, ok := store.Get(second.ID); !ok {
		t.Fatalf("expected live session %q to remain", second.ID)
	}
}

func TestSessionStoreEvictsOldestSessionAtCapacity(t *testing.T) {
	store := NewSessionStore()
	store.ttl = 0
	store.maxCount = 1

	first := store.Create("bundle_a", nil, &expert.Session{})
	first.LastAccess = time.Now().UTC().Add(-time.Minute)

	second := store.Create("bundle_b", nil, &expert.Session{})
	if second == nil {
		t.Fatal("expected second session")
	}
	if _, ok := store.Get(first.ID); ok {
		t.Fatalf("expected oldest session %q to be evicted", first.ID)
	}
	if _, ok := store.Get(second.ID); !ok {
		t.Fatalf("expected newest session %q to remain", second.ID)
	}
}

func TestSessionStoreRejectsOwnerOverCapacity(t *testing.T) {
	store := NewSessionStore()
	store.ttl = 0
	store.maxCount = 0
	store.maxOwner = 1

	first, err := store.CreateForOwner("token:test", "bundle_a", nil, &expert.Session{})
	if err != nil {
		t.Fatalf("CreateForOwner first: %v", err)
	}
	if first == nil {
		t.Fatal("expected first session")
	}
	second, err := store.CreateForOwner("token:test", "bundle_b", nil, &expert.Session{})
	if err == nil {
		t.Fatal("expected owner capacity error")
	}
	if second != nil {
		t.Fatal("expected rejected session to be nil")
	}
	if _, ok := store.Get(first.ID); !ok {
		t.Fatalf("expected first session %q to remain", first.ID)
	}
}

func TestSessionStoreStatusReportsBundleBreakdownAndLimits(t *testing.T) {
	store := NewSessionStore()
	store.SetTTL(45 * time.Minute)
	store.SetMaxCount(50)
	store.SetMaxPerOwner(4)

	if _, err := store.CreateForOwner("token:a", "bundle_a", nil, &expert.Session{}); err != nil {
		t.Fatalf("CreateForOwner bundle_a: %v", err)
	}
	if _, err := store.CreateForOwner("token:b", "bundle_b", nil, &expert.Session{}); err != nil {
		t.Fatalf("CreateForOwner bundle_b first: %v", err)
	}
	if _, err := store.CreateForOwner("token:c", "bundle_b", nil, &expert.Session{}); err != nil {
		t.Fatalf("CreateForOwner bundle_b second: %v", err)
	}

	status := store.Status()
	if status.Active != 3 || status.TTL != 45*time.Minute || status.MaxCount != 50 || status.MaxPerOwner != 4 {
		t.Fatalf("unexpected status header: %+v", status)
	}
	if len(status.Bundles) != 2 {
		t.Fatalf("unexpected bundle status count: %+v", status.Bundles)
	}
	if status.Bundles[0].BundleID != "bundle_a" || status.Bundles[0].Active != 1 {
		t.Fatalf("unexpected first bundle status: %+v", status.Bundles[0])
	}
	if status.Bundles[1].BundleID != "bundle_b" || status.Bundles[1].Active != 2 {
		t.Fatalf("unexpected second bundle status: %+v", status.Bundles[1])
	}
}
