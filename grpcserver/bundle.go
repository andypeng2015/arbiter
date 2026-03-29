package grpcserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/expert"
	"github.com/odvcencio/arbiter/flags"
)

type bundleEventType string

const (
	bundleEventPublished  bundleEventType = "published"
	bundleEventActivated  bundleEventType = "activated"
	bundleEventRolledBack bundleEventType = "rolled_back"
)

type bundleEvent struct {
	Type             bundleEventType
	Bundle           *Bundle
	PreviousBundleID string
}

// Bundle is a published governed artifact available over gRPC.
type Bundle struct {
	ID              string
	Name            string
	Checksum        string
	Source          []byte
	Published       time.Time
	Compiled        *arbiter.CompileResult
	Expert          *expert.Program
	Flags           *flags.Flags
	RuleCount       int
	ExpertRuleCount int
	FlagCount       int
	StrategyCount   int
}

type bundleRecord struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Checksum  string    `json:"checksum"`
	Source    []byte    `json:"source"`
	Published time.Time `json:"published"`
}

type registrySnapshot struct {
	Bundles []bundleRecord      `json:"bundles,omitempty"`
	History map[string][]string `json:"history,omitempty"`
	Active  map[string]string   `json:"active,omitempty"`
}

// PersistenceStatus reports the health of file-backed bundle persistence.
type PersistenceStatus struct {
	Configured    bool
	Durable       bool
	File          string
	Healthy       bool
	WritesTotal   uint64
	ErrorsTotal   uint64
	LastSuccessAt time.Time
	LastError     string
	LastErrorAt   time.Time
}

// Registry stores published bundles and optional active versions per bundle name.
type Registry struct {
	mu          sync.RWMutex
	bundles     map[string]*Bundle
	history     map[string][]string
	active      map[string]string
	subscribers map[uint64]chan bundleEvent
	nextSubID   uint64
	path        string
	persistence *persistenceTracker
}

type persistenceTracker struct {
	mu sync.RWMutex

	configured bool
	durable    bool
	file       string

	writesTotal   uint64
	errorsTotal   uint64
	lastSuccessAt time.Time
	lastError     string
	lastErrorAt   time.Time
}

func newPersistenceTracker() *persistenceTracker {
	return &persistenceTracker{}
}

func (t *persistenceTracker) Configure(configured bool, durable bool, file string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.configured != configured || t.durable != durable || t.file != file {
		t.writesTotal = 0
		t.errorsTotal = 0
		t.lastSuccessAt = time.Time{}
		t.lastError = ""
		t.lastErrorAt = time.Time{}
	}
	t.configured = configured
	t.durable = durable
	t.file = file
}

func (t *persistenceTracker) Snapshot() PersistenceStatus {
	if t == nil {
		return PersistenceStatus{Healthy: true}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.configured {
		return PersistenceStatus{Healthy: true}
	}
	healthy := true
	if !t.lastErrorAt.IsZero() && (t.lastSuccessAt.IsZero() || t.lastErrorAt.After(t.lastSuccessAt)) {
		healthy = false
	}
	return PersistenceStatus{
		Configured:    true,
		Durable:       t.durable,
		File:          t.file,
		Healthy:       healthy,
		WritesTotal:   t.writesTotal,
		ErrorsTotal:   t.errorsTotal,
		LastSuccessAt: t.lastSuccessAt,
		LastError:     t.lastError,
		LastErrorAt:   t.lastErrorAt,
	}
}

func (t *persistenceTracker) RecordSuccess(at time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.configured {
		return
	}
	t.writesTotal++
	t.lastSuccessAt = at.UTC()
}

func (t *persistenceTracker) RecordError(at time.Time, err error) {
	if t == nil || err == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.configured {
		return
	}
	t.errorsTotal++
	t.lastError = err.Error()
	t.lastErrorAt = at.UTC()
}

// BuildBundle compiles one bundle payload without mutating a registry.
func BuildBundle(name string, source []byte, published time.Time) (*Bundle, error) {
	if published.IsZero() {
		published = time.Now().UTC()
	} else {
		published = published.UTC()
	}
	return compileBundleRecord(bundleRecord{
		ID:        bundleIdentity(name, source),
		Name:      name,
		Checksum:  sourceChecksum(source),
		Source:    append([]byte(nil), source...),
		Published: published,
	})
}

// NewRegistry creates an empty in-memory bundle registry.
func NewRegistry() *Registry {
	return &Registry{
		bundles:     make(map[string]*Bundle),
		history:     make(map[string][]string),
		active:      make(map[string]string),
		subscribers: make(map[uint64]chan bundleEvent),
		persistence: newPersistenceTracker(),
	}
}

// NewFileRegistry loads and persists bundle state to one JSON file.
func NewFileRegistry(path string) (*Registry, error) {
	reg := NewRegistry()
	if err := reg.UseFile(path); err != nil {
		return nil, err
	}
	return reg, nil
}

// UseFile enables file-backed persistence for bundle state.
func (r *Registry) UseFile(path string) error {
	if path == "" {
		r.mu.Lock()
		r.path = ""
		r.mu.Unlock()
		r.persistence.Configure(false, false, "")
		return nil
	}
	r.persistence.Configure(true, true, path)
	if err := r.loadFileIfExists(path); err != nil {
		r.persistence.RecordError(time.Now().UTC(), err)
		return err
	}
	r.mu.Lock()
	r.path = path
	snapshot := r.snapshotLocked()
	r.mu.Unlock()
	if err := saveRegistrySnapshot(path, snapshot); err != nil {
		r.persistence.RecordError(time.Now().UTC(), err)
		return err
	}
	r.persistence.RecordSuccess(time.Now().UTC())
	return nil
}

// Publish compiles and stores a governed bundle. The newest published version
// becomes the active version for its name.
func (r *Registry) Publish(name string, source []byte) (*Bundle, error) {
	record := bundleRecord{
		ID:        bundleIdentity(name, source),
		Name:      name,
		Checksum:  sourceChecksum(source),
		Source:    append([]byte(nil), source...),
		Published: time.Now().UTC(),
	}

	r.mu.RLock()
	if existing, ok := r.bundles[record.ID]; ok {
		r.mu.RUnlock()
		return existing, nil
	}
	r.mu.RUnlock()

	bundle, err := compileBundleRecord(record)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	if existing, ok := r.bundles[bundle.ID]; ok {
		r.mu.Unlock()
		return existing, nil
	}
	r.bundles[bundle.ID] = bundle
	if !slices.Contains(r.history[bundle.Name], bundle.ID) {
		r.history[bundle.Name] = append(r.history[bundle.Name], bundle.ID)
	}
	r.active[bundle.Name] = bundle.ID
	snapshot := r.snapshotLocked()
	r.mu.Unlock()

	if err := r.persistSnapshot(snapshot); err != nil {
		return nil, err
	}
	r.notify(bundleEvent{
		Type:   bundleEventPublished,
		Bundle: bundle,
	})
	return bundle, nil
}

// Get returns a previously published bundle by ID.
func (r *Registry) Get(id string) (*Bundle, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	bundle, ok := r.bundles[id]
	return bundle, ok
}

// GetActive returns the active bundle for one bundle name.
func (r *Registry) GetActive(name string) (*Bundle, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.active[name]
	if !ok {
		return nil, false
	}
	bundle, ok := r.bundles[id]
	return bundle, ok
}

// Resolve returns a bundle by explicit ID or active bundle name.
func (r *Registry) Resolve(id, name string) (*Bundle, error) {
	if id != "" {
		if bundle, ok := r.Get(id); ok {
			return bundle, nil
		}
		return nil, fmt.Errorf("bundle %q not found", id)
	}
	if name != "" {
		if bundle, ok := r.GetActive(name); ok {
			return bundle, nil
		}
		return nil, fmt.Errorf("active bundle %q not found", name)
	}
	return nil, fmt.Errorf("bundle_id or bundle_name is required")
}

// List returns all bundles, optionally filtered by name, newest first.
func (r *Registry) List(name string) []*Bundle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name != "" {
		return listBundlesLocked(r.bundles, r.history, []string{name})
	}
	return listBundlesLocked(r.bundles, r.history, nil)
}

// ActiveBundles returns the active bundle for each requested name.
// If names is empty, it returns all active bundles.
func (r *Registry) ActiveBundles(names []string) []*Bundle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return activeBundlesLocked(r.bundles, r.active, names)
}

func activeBundlesLocked(bundles map[string]*Bundle, active map[string]string, names []string) []*Bundle {
	if len(names) == 0 {
		keys := make([]string, 0, len(active))
		for name := range active {
			keys = append(keys, name)
		}
		slices.Sort(keys)

		out := make([]*Bundle, 0, len(keys))
		for _, name := range keys {
			if id, ok := active[name]; ok {
				if bundle, ok := bundles[id]; ok {
					out = append(out, bundle)
				}
			}
		}
		return out
	}

	seen := make(map[string]struct{}, len(names))
	out := make([]*Bundle, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if id, ok := active[name]; ok {
			if bundle, ok := bundles[id]; ok {
				out = append(out, bundle)
			}
		}
	}
	return out
}

// Install stores a precompiled bundle and optionally activates it.
func (r *Registry) Install(bundle *Bundle, activate bool) (*Bundle, error) {
	if bundle == nil {
		return nil, errors.New("bundle is required")
	}
	if bundle.ID == "" || bundle.Name == "" {
		return nil, errors.New("bundle id and name are required")
	}

	published := false
	activated := false

	r.mu.Lock()
	stored := bundle
	if existing, ok := r.bundles[bundle.ID]; ok {
		stored = existing
	} else {
		r.bundles[bundle.ID] = bundle
		if !slices.Contains(r.history[bundle.Name], bundle.ID) {
			r.history[bundle.Name] = append(r.history[bundle.Name], bundle.ID)
		}
		published = true
	}
	if activate && r.active[stored.Name] != stored.ID {
		r.active[stored.Name] = stored.ID
		activated = true
	}
	snapshot := r.snapshotLocked()
	r.mu.Unlock()

	if err := r.persistSnapshot(snapshot); err != nil {
		return nil, err
	}
	if published {
		r.notify(bundleEvent{
			Type:   bundleEventPublished,
			Bundle: stored,
		})
	}
	if activated {
		r.notify(bundleEvent{
			Type:   bundleEventActivated,
			Bundle: stored,
		})
	}
	return stored, nil
}

// Activate switches the active bundle for one bundle name.
func (r *Registry) Activate(name, id string) (*Bundle, error) {
	r.mu.Lock()
	bundle, ok := r.bundles[id]
	if !ok {
		r.mu.Unlock()
		return nil, fmt.Errorf("bundle %q not found", id)
	}
	if bundle.Name != name {
		r.mu.Unlock()
		return nil, fmt.Errorf("bundle %q belongs to %q, not %q", id, bundle.Name, name)
	}
	r.active[name] = id
	snapshot := r.snapshotLocked()
	r.mu.Unlock()
	if err := r.persistSnapshot(snapshot); err != nil {
		return nil, err
	}
	r.notify(bundleEvent{
		Type:   bundleEventActivated,
		Bundle: bundle,
	})
	return bundle, nil
}

// Rollback switches the active bundle to the previous published version.
func (r *Registry) Rollback(name string) (*Bundle, *Bundle, error) {
	r.mu.Lock()
	history := r.history[name]
	if len(history) < 2 {
		r.mu.Unlock()
		return nil, nil, fmt.Errorf("bundle %q has no previous version", name)
	}
	currentID := r.active[name]
	currentPos := -1
	for i, id := range history {
		if id == currentID {
			currentPos = i
			break
		}
	}
	if currentPos <= 0 {
		r.mu.Unlock()
		return nil, nil, fmt.Errorf("bundle %q has no previous version", name)
	}
	previousID := history[currentPos-1]
	current := r.bundles[currentID]
	previous := r.bundles[previousID]
	if previous == nil {
		r.mu.Unlock()
		return nil, nil, fmt.Errorf("previous bundle %q not found", previousID)
	}
	r.active[name] = previousID
	snapshot := r.snapshotLocked()
	r.mu.Unlock()
	if err := r.persistSnapshot(snapshot); err != nil {
		return nil, nil, err
	}
	r.notify(bundleEvent{
		Type:             bundleEventRolledBack,
		Bundle:           previous,
		PreviousBundleID: currentID,
	})
	return previous, current, nil
}

// Subscribe registers for bundle change events.
func (r *Registry) Subscribe() (<-chan bundleEvent, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.subscribers == nil {
		r.subscribers = make(map[uint64]chan bundleEvent)
	}
	id := r.nextSubID
	r.nextSubID++
	ch := make(chan bundleEvent, 4)
	r.subscribers[id] = ch
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.subscribers, id)
			r.mu.Unlock()
		})
	}
	return ch, cancel
}

// SubscribeActive atomically captures the current active bundles and registers
// for future bundle change events.
func (r *Registry) SubscribeActive(names []string) ([]*Bundle, <-chan bundleEvent, func()) {
	return r.SubscribeBundles(names, true)
}

// SubscribeBundles atomically captures the current bundle snapshot and registers
// for future bundle change events.
func (r *Registry) SubscribeBundles(names []string, activeOnly bool) ([]*Bundle, <-chan bundleEvent, func()) {
	r.mu.Lock()
	if r.subscribers == nil {
		r.subscribers = make(map[uint64]chan bundleEvent)
	}
	id := r.nextSubID
	r.nextSubID++
	ch := make(chan bundleEvent, 4)
	r.subscribers[id] = ch
	initial := activeBundlesLocked(r.bundles, r.active, names)
	if !activeOnly {
		initial = listBundlesLocked(r.bundles, r.history, names)
	}
	r.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.subscribers, id)
			r.mu.Unlock()
		})
	}
	return initial, ch, cancel
}

func listBundlesLocked(bundles map[string]*Bundle, history map[string][]string, names []string) []*Bundle {
	var out []*Bundle
	if len(names) == 0 {
		out = make([]*Bundle, 0, len(bundles))
		for _, bundle := range bundles {
			out = append(out, bundle)
		}
	} else {
		seen := make(map[string]struct{}, len(names))
		for _, name := range names {
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			for _, id := range history[name] {
				if bundle, ok := bundles[id]; ok {
					out = append(out, bundle)
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b *Bundle) int {
		switch {
		case a.Published.After(b.Published):
			return -1
		case a.Published.Before(b.Published):
			return 1
		default:
			return 0
		}
	})
	return out
}

func (r *Registry) notify(event bundleEvent) {
	r.mu.RLock()
	subscribers := make([]chan bundleEvent, 0, len(r.subscribers))
	for _, ch := range r.subscribers {
		subscribers = append(subscribers, ch)
	}
	r.mu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- event:
			continue
		default:
		}
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- event:
		default:
		}
	}
}

func (r *Registry) persistSnapshot(snapshot registrySnapshot) error {
	if r == nil || r.path == "" {
		return nil
	}
	err := saveRegistrySnapshot(r.path, snapshot)
	now := time.Now().UTC()
	if err != nil {
		r.persistence.RecordError(now, err)
		return err
	}
	r.persistence.RecordSuccess(now)
	return nil
}

// PersistenceStatus reports the health of the registry's file-backed persistence.
func (r *Registry) PersistenceStatus() PersistenceStatus {
	if r == nil {
		return PersistenceStatus{Healthy: true}
	}
	return r.persistence.Snapshot()
}

func (r *Registry) snapshotLocked() registrySnapshot {
	out := registrySnapshot{
		Bundles: make([]bundleRecord, 0, len(r.bundles)),
		History: make(map[string][]string, len(r.history)),
		Active:  make(map[string]string, len(r.active)),
	}
	for _, bundle := range r.bundles {
		out.Bundles = append(out.Bundles, bundleRecord{
			ID:        bundle.ID,
			Name:      bundle.Name,
			Checksum:  bundle.Checksum,
			Source:    append([]byte(nil), bundle.Source...),
			Published: bundle.Published,
		})
	}
	for name, ids := range r.history {
		out.History[name] = append([]string(nil), ids...)
	}
	for name, id := range r.active {
		out.Active[name] = id
	}
	return out
}

func (r *Registry) loadFileIfExists(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var snapshot registrySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	bundles := make(map[string]*Bundle, len(snapshot.Bundles))
	for _, record := range snapshot.Bundles {
		bundle, err := compileBundleRecord(record)
		if err != nil {
			return fmt.Errorf("load bundle %s: %w", record.ID, err)
		}
		bundles[bundle.ID] = bundle
	}
	r.mu.Lock()
	r.bundles = bundles
	r.history = make(map[string][]string, len(snapshot.History))
	for name, ids := range snapshot.History {
		r.history[name] = append([]string(nil), ids...)
	}
	r.active = make(map[string]string, len(snapshot.Active))
	for name, id := range snapshot.Active {
		r.active[name] = id
	}
	r.mu.Unlock()
	return nil
}

func compileBundleRecord(record bundleRecord) (*Bundle, error) {
	parsed, err := arbiter.ParseSource(record.Source)
	if err != nil {
		return nil, fmt.Errorf("parse bundle: %w", err)
	}
	compiled, err := arbiter.CompileFullParsed(parsed)
	if err != nil {
		return nil, fmt.Errorf("compile rules: %w", err)
	}
	flagSet, err := flags.LoadParsed(parsed, compiled)
	if err != nil {
		return nil, fmt.Errorf("compile flags: %w", err)
	}
	expertProgram, err := expert.CompileParsed(parsed, compiled)
	if err != nil {
		return nil, fmt.Errorf("compile expert rules: %w", err)
	}
	return &Bundle{
		ID:              record.ID,
		Name:            record.Name,
		Checksum:        record.Checksum,
		Source:          append([]byte(nil), record.Source...),
		Published:       record.Published,
		Compiled:        compiled,
		Expert:          expertProgram,
		Flags:           flagSet,
		RuleCount:       len(compiled.Ruleset.Rules),
		ExpertRuleCount: len(expertProgram.Rules()),
		FlagCount:       flagSet.Count(),
		StrategyCount:   compiled.Strategies.Count(),
	}, nil
}

func bundleIdentity(name string, source []byte) string {
	sum := sha256.Sum256(append(append([]byte(name), 0), source...))
	return hex.EncodeToString(sum[:])[:16]
}

func sourceChecksum(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}

func saveRegistrySnapshot(path string, snapshot registrySnapshot) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
