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
