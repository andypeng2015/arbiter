package main

import (
	"context"
	"crypto/tls"
	"slices"
	"sort"
	"strings"
	"time"

	arbiterv1 "m31labs.dev/arbiter/api/arbiter/v1"
	"m31labs.dev/arbiter/grpcserver"
	"m31labs.dev/arbiter/internal/buildinfo"
	"m31labs.dev/arbiter/internal/grpcutil"
	"m31labs.dev/arbiter/internal/statusview"
	"m31labs.dev/arbiter/overrides"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type controlListenerTransport struct {
	Enabled          bool   `json:"enabled"`
	Address          string `json:"address,omitempty"`
	PublicListener   bool   `json:"public_listener,omitempty"`
	AuthEnabled      bool   `json:"auth_enabled"`
	TLSEnabled       bool   `json:"tls_enabled"`
	MutualTLSEnabled bool   `json:"mutual_tls_enabled"`
}

type controlTransportStatus struct {
	Control controlListenerTransport `json:"control"`
}

type controlReadinessStatus struct {
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

type controlBundleStatus struct {
	Name              string    `json:"name"`
	BundleID          string    `json:"bundle_id"`
	Checksum          string    `json:"checksum,omitempty"`
	PublishedAt       time.Time `json:"published_at,omitempty"`
	PublishedVersions int       `json:"published_versions"`
	RuleCount         int       `json:"rule_count"`
	FlagCount         int       `json:"flag_count"`
	ExpertRuleCount   int       `json:"expert_rule_count"`
	StrategyCount     int       `json:"strategy_count"`
}

type controlBundlesStatus struct {
	PublishedTotal int                   `json:"published_total"`
	ActiveTotal    int                   `json:"active_total"`
	Persisted      bool                  `json:"persisted"`
	File           string                `json:"file,omitempty"`
	Healthy        bool                  `json:"healthy"`
	WritesTotal    uint64                `json:"writes_total"`
	ErrorsTotal    uint64                `json:"errors_total"`
	LastSuccessAt  time.Time             `json:"last_success_at,omitempty"`
	LastError      string                `json:"last_error,omitempty"`
	LastErrorAt    time.Time             `json:"last_error_at,omitempty"`
	Active         []controlBundleStatus `json:"active,omitempty"`
}

type controlBundleOverrideStatus struct {
	Name       string `json:"name,omitempty"`
	BundleID   string `json:"bundle_id"`
	Rules      int    `json:"rules"`
	Flags      int    `json:"flags"`
	FlagRules  int    `json:"flag_rules"`
	Strategies int    `json:"strategies"`
}

type controlOverridesStatus struct {
	BundleTotal   int                           `json:"bundle_total"`
	Rules         int                           `json:"rules"`
	Flags         int                           `json:"flags"`
	FlagRules     int                           `json:"flag_rules"`
	Strategies    int                           `json:"strategies"`
	Persisted     bool                          `json:"persisted"`
	File          string                        `json:"file,omitempty"`
	Healthy       bool                          `json:"healthy"`
	WritesTotal   uint64                        `json:"writes_total"`
	ErrorsTotal   uint64                        `json:"errors_total"`
	LastSuccessAt time.Time                     `json:"last_success_at,omitempty"`
	LastError     string                        `json:"last_error,omitempty"`
	LastErrorAt   time.Time                     `json:"last_error_at,omitempty"`
	Bundles       []controlBundleOverrideStatus `json:"bundles,omitempty"`
}

type controlSessionBundleStatus struct {
	Name     string `json:"name,omitempty"`
	BundleID string `json:"bundle_id"`
	Active   int    `json:"active"`
}

type controlSessionsStatus struct {
	Active      int                          `json:"active"`
	TTLMS       int64                        `json:"ttl_ms"`
	MaxCount    int                          `json:"max_count"`
	MaxPerOwner int                          `json:"max_per_owner"`
	Bundles     []controlSessionBundleStatus `json:"bundles,omitempty"`
}

type controlAuditStatus struct {
	Configured    bool      `json:"configured"`
	Kind          string    `json:"kind"`
	Durable       bool      `json:"durable"`
	File          string    `json:"file,omitempty"`
	Healthy       bool      `json:"healthy"`
	WritesTotal   uint64    `json:"writes_total"`
	ErrorsTotal   uint64    `json:"errors_total"`
	LastSuccessAt time.Time `json:"last_success_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	LastErrorAt   time.Time `json:"last_error_at,omitempty"`
}

type controlStatusPayload struct {
	Operator  buildinfo.OperatorInfo `json:"operator"`
	Readiness controlReadinessStatus `json:"readiness"`
	Issues    []statusview.Issue     `json:"issues"`
	Transport controlTransportStatus `json:"transport"`
	Bundles   controlBundlesStatus   `json:"bundles"`
	Overrides controlOverridesStatus `json:"overrides"`
	Sessions  controlSessionsStatus  `json:"sessions"`
	Audit     controlAuditStatus     `json:"audit"`
}

type controlStatusSource struct {
	registry      *grpcserver.Registry
	store         *overrides.Store
	sessions      *grpcserver.SessionStore
	transport     controlListenerTransport
	bundleFile    string
	overridesFile string
	audit         *controlAuditTracker
}

func newControlListenerTransport(address string, tokens []string, tlsConfig *tls.Config) controlListenerTransport {
	status := controlListenerTransport{
		Enabled:        address != "",
		Address:        address,
		PublicListener: grpcutil.IsPublicListenAddr(address),
		AuthEnabled:    len(tokens) > 0,
		TLSEnabled:     tlsConfig != nil,
	}
	if tlsConfig != nil && tlsConfig.ClientAuth == tls.RequireAndVerifyClientCert {
		status.MutualTLSEnabled = true
	}
	return status
}

func (s controlStatusSource) Payload() controlStatusPayload {
	return newControlStatusPayload(s.registry, s.store, s.sessions, s.transport, s.bundleFile, s.overridesFile, s.audit)
}

func newControlStatusPayload(
	registry *grpcserver.Registry,
	store *overrides.Store,
	sessions *grpcserver.SessionStore,
	transport controlListenerTransport,
	bundleFile string,
	overridesFile string,
	audit *controlAuditTracker,
) controlStatusPayload {
	bundles := controlBundlesPayload(registry, bundleFile)
	overrideStatus := controlOverridesPayload(registry, store, overridesFile)
	sessionStatus := controlSessionsPayload(registry, sessions)
	auditStatus := controlAuditPayload(audit)
	issues := controlIssues(registry != nil && store != nil && sessions != nil, transport, bundles, overrideStatus, auditStatus)
	return controlStatusPayload{
		Operator:  buildinfo.Current(),
		Readiness: controlReadinessPayload(registry != nil && store != nil && sessions != nil, bundles, overrideStatus, auditStatus),
		Issues:    issues,
		Transport: controlTransportStatus{Control: transport},
		Bundles:   bundles,
		Overrides: overrideStatus,
		Sessions:  sessionStatus,
		Audit:     auditStatus,
	}
}

func controlReadinessPayload(available bool, bundles controlBundlesStatus, overrides controlOverridesStatus, audit controlAuditStatus) controlReadinessStatus {
	if !available {
		return controlReadinessStatus{
			Ready:  false,
			Reason: "status unavailable",
		}
	}
	if bundles.Persisted && !bundles.Healthy {
		return controlReadinessStatus{
			Ready:  false,
			Reason: "bundle persistence unhealthy",
		}
	}
	if overrides.Persisted && !overrides.Healthy {
		return controlReadinessStatus{
			Ready:  false,
			Reason: "override persistence unhealthy",
		}
	}
	if audit.Configured && !audit.Healthy {
		return controlReadinessStatus{
			Ready:  false,
			Reason: "audit unhealthy",
		}
	}
	return controlReadinessStatus{Ready: true}
}

func controlIssues(available bool, transport controlListenerTransport, bundles controlBundlesStatus, overrides controlOverridesStatus, audit controlAuditStatus) []statusview.Issue {
	issues := make([]statusview.Issue, 0)
	if !available {
		return append(issues, statusview.New(statusview.CodeStatusUnavailable, "control", "status unavailable"))
	}
	if transport.PublicListener && !transport.TLSEnabled && !transport.AuthEnabled {
		issues = append(issues, statusview.New(statusview.CodePublicControlInsecure, controlIssueSubject(transport.Address, "control"), "public control listener has no TLS or auth"))
	}
	if bundles.Persisted && !bundles.Healthy {
		issues = append(issues, statusview.New(statusview.CodeBundlePersistenceUnhealthy, controlIssueSubject(bundles.File, "bundles"), controlIssueMessage("bundle persistence unhealthy", bundles.LastError)))
	}
	if overrides.Persisted && !overrides.Healthy {
		issues = append(issues, statusview.New(statusview.CodeOverridePersistenceUnhealthy, controlIssueSubject(overrides.File, "overrides"), controlIssueMessage("override persistence unhealthy", overrides.LastError)))
	}
	if audit.Configured && !audit.Healthy {
		issues = append(issues, statusview.New(statusview.CodeAuditUnhealthy, controlIssueSubject(audit.File, audit.Kind, "audit"), controlIssueMessage("audit unhealthy", audit.LastError)))
	}
	return issues
}

func controlIssueSubject(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return "unknown"
}

func controlIssueMessage(base string, detail string) string {
	base = strings.TrimSpace(base)
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return base
	}
	return base + ": " + detail
}

func controlAuditPayload(audit *controlAuditTracker) controlAuditStatus {
	if audit == nil {
		return controlAuditStatus{
			Configured: false,
			Kind:       "discard",
			Durable:    false,
			Healthy:    true,
		}
	}
	return audit.Snapshot()
}

func controlBundlesPayload(registry *grpcserver.Registry, bundleFile string) controlBundlesStatus {
	status := controlBundlesStatus{
		Persisted: bundleFile != "",
		File:      bundleFile,
		Healthy:   true,
	}
	if registry == nil {
		return status
	}
	persistence := registry.PersistenceStatus()
	status.Persisted = persistence.Configured
	status.File = persistence.File
	status.Healthy = persistence.Healthy
	status.WritesTotal = persistence.WritesTotal
	status.ErrorsTotal = persistence.ErrorsTotal
	status.LastSuccessAt = persistence.LastSuccessAt
	status.LastError = persistence.LastError
	status.LastErrorAt = persistence.LastErrorAt

	published := registry.List("")
	active := registry.ActiveBundles(nil)
	status.PublishedTotal = len(published)
	status.ActiveTotal = len(active)
	if len(active) == 0 {
		return status
	}

	versions := make(map[string]int, len(active))
	for _, bundle := range published {
		if bundle == nil {
			continue
		}
		versions[bundle.Name]++
	}

	status.Active = make([]controlBundleStatus, 0, len(active))
	for _, bundle := range active {
		if bundle == nil {
			continue
		}
		status.Active = append(status.Active, controlBundleStatus{
			Name:              bundle.Name,
			BundleID:          bundle.ID,
			Checksum:          bundle.Checksum,
			PublishedAt:       bundle.Published,
			PublishedVersions: versions[bundle.Name],
			RuleCount:         bundle.RuleCount,
			FlagCount:         bundle.FlagCount,
			ExpertRuleCount:   bundle.ExpertRuleCount,
			StrategyCount:     bundle.StrategyCount,
		})
	}
	return status
}

func controlOverridesPayload(registry *grpcserver.Registry, store *overrides.Store, overridesFile string) controlOverridesStatus {
	status := controlOverridesStatus{
		Persisted: overridesFile != "",
		File:      overridesFile,
		Healthy:   true,
	}
	if store == nil {
		return status
	}
	persistence := store.PersistenceStatus()
	status.Persisted = persistence.Configured
	status.File = persistence.File
	status.Healthy = persistence.Healthy
	status.WritesTotal = persistence.WritesTotal
	status.ErrorsTotal = persistence.ErrorsTotal
	status.LastSuccessAt = persistence.LastSuccessAt
	status.LastError = persistence.LastError
	status.LastErrorAt = persistence.LastErrorAt

	snapshot := store.Snapshot()
	bundleIDs := make(map[string]struct{})
	for bundleID, rules := range snapshot.Rules {
		status.Rules += len(rules)
		bundleIDs[bundleID] = struct{}{}
	}
	for bundleID, flags := range snapshot.Flags {
		status.Flags += len(flags)
		bundleIDs[bundleID] = struct{}{}
	}
	for bundleID, flags := range snapshot.FlagRules {
		for _, rules := range flags {
			status.FlagRules += len(rules)
		}
		bundleIDs[bundleID] = struct{}{}
	}
	for bundleID, strategies := range snapshot.Strategies {
		for _, candidates := range strategies {
			status.Strategies += len(candidates)
		}
		bundleIDs[bundleID] = struct{}{}
	}
	status.BundleTotal = len(bundleIDs)
	if len(bundleIDs) == 0 {
		return status
	}

	keys := make([]string, 0, len(bundleIDs))
	for bundleID := range bundleIDs {
		keys = append(keys, bundleID)
	}
	slices.Sort(keys)

	status.Bundles = make([]controlBundleOverrideStatus, 0, len(keys))
	for _, bundleID := range keys {
		item := controlBundleOverrideStatus{BundleID: bundleID}
		if registry != nil {
			if bundle, ok := registry.Get(bundleID); ok && bundle != nil {
				item.Name = bundle.Name
			}
		}
		if rules, ok := snapshot.Rules[bundleID]; ok {
			item.Rules = len(rules)
		}
		if flags, ok := snapshot.Flags[bundleID]; ok {
			item.Flags = len(flags)
		}
		if flags, ok := snapshot.FlagRules[bundleID]; ok {
			for _, rules := range flags {
				item.FlagRules += len(rules)
			}
		}
		if strategies, ok := snapshot.Strategies[bundleID]; ok {
			for _, candidates := range strategies {
				item.Strategies += len(candidates)
			}
		}
		status.Bundles = append(status.Bundles, item)
	}
	sort.Slice(status.Bundles, func(i, j int) bool {
		left, right := status.Bundles[i], status.Bundles[j]
		if left.Name == right.Name {
			return left.BundleID < right.BundleID
		}
		if left.Name == "" {
			return false
		}
		if right.Name == "" {
			return true
		}
		return left.Name < right.Name
	})
	return status
}

func controlSessionsPayload(registry *grpcserver.Registry, sessions *grpcserver.SessionStore) controlSessionsStatus {
	if sessions == nil {
		return controlSessionsStatus{}
	}
	snapshot := sessions.Status()
	status := controlSessionsStatus{
		Active:      snapshot.Active,
		TTLMS:       snapshot.TTL.Milliseconds(),
		MaxCount:    snapshot.MaxCount,
		MaxPerOwner: snapshot.MaxPerOwner,
	}
	if len(snapshot.Bundles) == 0 {
		return status
	}

	status.Bundles = make([]controlSessionBundleStatus, 0, len(snapshot.Bundles))
	for _, item := range snapshot.Bundles {
		entry := controlSessionBundleStatus{
			BundleID: item.BundleID,
			Active:   item.Active,
		}
		if registry != nil {
			if bundle, ok := registry.Get(item.BundleID); ok && bundle != nil {
				entry.Name = bundle.Name
			}
		}
		status.Bundles = append(status.Bundles, entry)
	}
	sort.Slice(status.Bundles, func(i, j int) bool {
		left, right := status.Bundles[i], status.Bundles[j]
		if left.Name == right.Name {
			return left.BundleID < right.BundleID
		}
		if left.Name == "" {
			return false
		}
		if right.Name == "" {
			return true
		}
		return left.Name < right.Name
	})
	return status
}

type controlRPCServer struct {
	arbiterv1.UnimplementedControlServiceServer
	source controlStatusSource
}

func newControlRPCServer(source controlStatusSource) *controlRPCServer {
	return &controlRPCServer{source: source}
}

func (s *controlRPCServer) GetControlStatus(context.Context, *arbiterv1.GetControlStatusRequest) (*arbiterv1.GetControlStatusResponse, error) {
	return protoControlStatus(s.source.Payload()), nil
}

func (*controlRPCServer) GetStatusIssueCatalog(context.Context, *arbiterv1.GetStatusIssueCatalogRequest) (*arbiterv1.GetStatusIssueCatalogResponse, error) {
	return statusview.ProtoCatalog(statusview.SurfaceControl), nil
}

func protoControlStatus(payload controlStatusPayload) *arbiterv1.GetControlStatusResponse {
	resp := &arbiterv1.GetControlStatusResponse{
		Operator: statusview.ProtoOperator(),
		Readiness: &arbiterv1.ControlReadinessStatus{
			Ready:  payload.Readiness.Ready,
			Reason: payload.Readiness.Reason,
		},
		Issues: statusview.ProtoIssues(payload.Issues),
		Transport: &arbiterv1.ControlTransportStatus{
			Control: &arbiterv1.ControlListenerTransport{
				Enabled:          payload.Transport.Control.Enabled,
				Address:          payload.Transport.Control.Address,
				PublicListener:   payload.Transport.Control.PublicListener,
				AuthEnabled:      payload.Transport.Control.AuthEnabled,
				TlsEnabled:       payload.Transport.Control.TLSEnabled,
				MutualTlsEnabled: payload.Transport.Control.MutualTLSEnabled,
			},
		},
		Bundles: &arbiterv1.ControlBundlesStatus{
			PublishedTotal: uint32(payload.Bundles.PublishedTotal),
			ActiveTotal:    uint32(payload.Bundles.ActiveTotal),
			Persisted:      payload.Bundles.Persisted,
			File:           payload.Bundles.File,
			Healthy:        payload.Bundles.Healthy,
			WritesTotal:    payload.Bundles.WritesTotal,
			ErrorsTotal:    payload.Bundles.ErrorsTotal,
			LastSuccessAt:  protoControlTimestamp(payload.Bundles.LastSuccessAt),
			LastError:      payload.Bundles.LastError,
			LastErrorAt:    protoControlTimestamp(payload.Bundles.LastErrorAt),
			Active:         make([]*arbiterv1.ControlBundleStatus, 0, len(payload.Bundles.Active)),
		},
		Overrides: &arbiterv1.ControlOverridesStatus{
			BundleTotal:   uint32(payload.Overrides.BundleTotal),
			Rules:         uint32(payload.Overrides.Rules),
			Flags:         uint32(payload.Overrides.Flags),
			FlagRules:     uint32(payload.Overrides.FlagRules),
			Strategies:    uint32(payload.Overrides.Strategies),
			Persisted:     payload.Overrides.Persisted,
			File:          payload.Overrides.File,
			Healthy:       payload.Overrides.Healthy,
			WritesTotal:   payload.Overrides.WritesTotal,
			ErrorsTotal:   payload.Overrides.ErrorsTotal,
			LastSuccessAt: protoControlTimestamp(payload.Overrides.LastSuccessAt),
			LastError:     payload.Overrides.LastError,
			LastErrorAt:   protoControlTimestamp(payload.Overrides.LastErrorAt),
			Bundles:       make([]*arbiterv1.ControlBundleOverrideStatus, 0, len(payload.Overrides.Bundles)),
		},
		Sessions: &arbiterv1.ControlSessionsStatus{
			Active:      uint32(payload.Sessions.Active),
			TtlMs:       payload.Sessions.TTLMS,
			MaxCount:    uint32(payload.Sessions.MaxCount),
			MaxPerOwner: uint32(payload.Sessions.MaxPerOwner),
			Bundles:     make([]*arbiterv1.ControlSessionBundleStatus, 0, len(payload.Sessions.Bundles)),
		},
		Audit: &arbiterv1.ControlAuditStatus{
			Configured:    payload.Audit.Configured,
			Kind:          payload.Audit.Kind,
			Durable:       payload.Audit.Durable,
			File:          payload.Audit.File,
			Healthy:       payload.Audit.Healthy,
			WritesTotal:   payload.Audit.WritesTotal,
			ErrorsTotal:   payload.Audit.ErrorsTotal,
			LastSuccessAt: protoControlTimestamp(payload.Audit.LastSuccessAt),
			LastError:     payload.Audit.LastError,
			LastErrorAt:   protoControlTimestamp(payload.Audit.LastErrorAt),
		},
	}

	for _, item := range payload.Bundles.Active {
		resp.Bundles.Active = append(resp.Bundles.Active, &arbiterv1.ControlBundleStatus{
			Name:              item.Name,
			BundleId:          item.BundleID,
			Checksum:          item.Checksum,
			PublishedAt:       protoControlTimestamp(item.PublishedAt),
			PublishedVersions: uint32(item.PublishedVersions),
			RuleCount:         uint32(item.RuleCount),
			FlagCount:         uint32(item.FlagCount),
			ExpertRuleCount:   uint32(item.ExpertRuleCount),
			StrategyCount:     uint32(item.StrategyCount),
		})
	}
	for _, item := range payload.Overrides.Bundles {
		resp.Overrides.Bundles = append(resp.Overrides.Bundles, &arbiterv1.ControlBundleOverrideStatus{
			Name:       item.Name,
			BundleId:   item.BundleID,
			Rules:      uint32(item.Rules),
			Flags:      uint32(item.Flags),
			FlagRules:  uint32(item.FlagRules),
			Strategies: uint32(item.Strategies),
		})
	}
	for _, item := range payload.Sessions.Bundles {
		resp.Sessions.Bundles = append(resp.Sessions.Bundles, &arbiterv1.ControlSessionBundleStatus{
			Name:     item.Name,
			BundleId: item.BundleID,
			Active:   uint32(item.Active),
		})
	}
	return resp
}

func protoControlTimestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}
