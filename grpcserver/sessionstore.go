package grpcserver

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/odvcencio/arbiter/expert"
)

// SessionStore keeps expert sessions in memory.
type SessionStore struct {
	mu       sync.RWMutex
	nextID   uint64
	sessions map[string]*ExpertSession
	ttl      time.Duration
	maxCount int
	maxOwner int
}

// ExpertSession is one live expert session.
type ExpertSession struct {
	mu         sync.Mutex
	ID         string
	Owner      string
	BundleID   string
	Envelope   map[string]any
	Session    *expert.Session
	CreatedAt  time.Time
	LastAccess time.Time
	closed     atomic.Bool
}

// SessionBundleStatus summarizes live sessions grouped by bundle.
type SessionBundleStatus struct {
	BundleID string
	Active   int
}

// SessionStatus summarizes the live expert-session surface.
type SessionStatus struct {
	Active      int
	TTL         time.Duration
	MaxCount    int
	MaxPerOwner int
	Bundles     []SessionBundleStatus
}

// NewSessionStore creates an empty expert-session store.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*ExpertSession),
		ttl:      30 * time.Minute,
		maxCount: 10_000,
		maxOwner: 100,
	}
}

// Create registers a new session and returns it.
func (ss *SessionStore) Create(bundleID string, envelope map[string]any, session *expert.Session) *ExpertSession {
	handle, _ := ss.CreateForOwner("", bundleID, envelope, session)
	return handle
}

// CreateForOwner registers a new session for one owner identity.
func (ss *SessionStore) CreateForOwner(owner, bundleID string, envelope map[string]any, session *expert.Session) (*ExpertSession, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	now := time.Now().UTC()
	ss.pruneExpiredLocked(now)
	if err := ss.ensureOwnerCapacityLocked(owner); err != nil {
		return nil, err
	}
	ss.evictIfNeededLocked()

	ss.nextID++
	id := fmt.Sprintf("sess_%d", ss.nextID)
	handle := &ExpertSession{
		ID:         id,
		Owner:      owner,
		BundleID:   bundleID,
		Envelope:   cloneMap(envelope),
		Session:    session,
		CreatedAt:  now,
		LastAccess: now,
	}
	ss.sessions[id] = handle
	return handle, nil
}

// Get returns a session by ID.
func (ss *SessionStore) Get(id string) (*ExpertSession, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.pruneExpiredLocked(time.Now().UTC())
	handle, ok := ss.sessions[id]
	if ok && !handle.closed.Load() {
		handle.LastAccess = time.Now().UTC()
		return handle, true
	}
	return nil, false
}

// Delete removes a session by ID.
func (ss *SessionStore) Delete(id string) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	handle, ok := ss.sessions[id]
	if !ok {
		return false
	}
	handle.closed.Store(true)
	delete(ss.sessions, id)
	return true
}

// Close removes a specific live session handle.
func (ss *SessionStore) Close(handle *ExpertSession) bool {
	if handle == nil {
		return false
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	current, ok := ss.sessions[handle.ID]
	if !ok || current != handle {
		return false
	}
	handle.closed.Store(true)
	delete(ss.sessions, handle.ID)
	return true
}

func (ss *SessionStore) pruneExpiredLocked(now time.Time) {
	if ss.ttl <= 0 {
		return
	}
	for id, handle := range ss.sessions {
		if now.Sub(handle.LastAccess) > ss.ttl {
			handle.closed.Store(true)
			delete(ss.sessions, id)
		}
	}
}

func (ss *SessionStore) evictIfNeededLocked() {
	if ss.maxCount <= 0 || len(ss.sessions) < ss.maxCount {
		return
	}
	var oldestID string
	var oldest time.Time
	for id, handle := range ss.sessions {
		if oldestID == "" || handle.LastAccess.Before(oldest) {
			oldestID = id
			oldest = handle.LastAccess
		}
	}
	if oldestID != "" {
		ss.sessions[oldestID].closed.Store(true)
		delete(ss.sessions, oldestID)
	}
}

// SetTTL configures the session expiry window. Zero disables TTL pruning.
func (ss *SessionStore) SetTTL(ttl time.Duration) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.ttl = ttl
}

// SetMaxCount configures the global live-session cap. Zero disables eviction.
func (ss *SessionStore) SetMaxCount(maxCount int) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.maxCount = maxCount
}

// SetMaxPerOwner configures the maximum number of live sessions one owner may hold.
// Zero disables the per-owner cap.
func (ss *SessionStore) SetMaxPerOwner(maxOwner int) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.maxOwner = maxOwner
}

// Status reports the current live-session surface after pruning expired entries.
func (ss *SessionStore) Status() SessionStatus {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.pruneExpiredLocked(time.Now().UTC())

	status := SessionStatus{
		TTL:         ss.ttl,
		MaxCount:    ss.maxCount,
		MaxPerOwner: ss.maxOwner,
	}
	if len(ss.sessions) == 0 {
		return status
	}

	byBundle := make(map[string]int, len(ss.sessions))
	for _, handle := range ss.sessions {
		if handle.closed.Load() {
			continue
		}
		status.Active++
		byBundle[handle.BundleID]++
	}
	if len(byBundle) == 0 {
		return status
	}

	keys := make([]string, 0, len(byBundle))
	for bundleID := range byBundle {
		keys = append(keys, bundleID)
	}
	slices.Sort(keys)
	status.Bundles = make([]SessionBundleStatus, 0, len(keys))
	for _, bundleID := range keys {
		status.Bundles = append(status.Bundles, SessionBundleStatus{
			BundleID: bundleID,
			Active:   byBundle[bundleID],
		})
	}
	return status
}

func (ss *SessionStore) ensureOwnerCapacityLocked(owner string) error {
	if ss.maxOwner <= 0 || owner == "" {
		return nil
	}
	count := 0
	for _, handle := range ss.sessions {
		if handle.Owner == owner && !handle.closed.Load() {
			count++
		}
	}
	if count >= ss.maxOwner {
		return fmt.Errorf("owner %q reached the maximum of %d live sessions", owner, ss.maxOwner)
	}
	return nil
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
