package main

import (
	"crypto/tls"

	"github.com/odvcencio/arbiter/internal/grpcutil"
)

type agentControlTransport struct {
	Enabled          bool   `json:"enabled"`
	Address          string `json:"address,omitempty"`
	PublicListener   bool   `json:"public_listener,omitempty"`
	AuthEnabled      bool   `json:"auth_enabled"`
	TLSEnabled       bool   `json:"tls_enabled"`
	MutualTLSEnabled bool   `json:"mutual_tls_enabled"`
}

type agentUpstreamTransport struct {
	Configured  bool   `json:"configured"`
	Target      string `json:"target,omitempty"`
	AuthEnabled bool   `json:"auth_enabled"`
	TLSEnabled  bool   `json:"tls_enabled"`
	ServerName  string `json:"server_name,omitempty"`
}

type agentTransportStatus struct {
	Control  agentControlTransport  `json:"control"`
	Upstream agentUpstreamTransport `json:"upstream"`
}

type agentReadinessStatus struct {
	Ready          bool   `json:"ready"`
	Reason         string `json:"reason,omitempty"`
	MaxStalenessMs int64  `json:"max_staleness_ms"`
	TargetCount    int    `json:"target_count"`
	ReadyCount     int    `json:"ready_count"`
}

func newAgentControlTransport(address string, tokens []string, tlsConfig *tls.Config) agentControlTransport {
	status := agentControlTransport{
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

func newAgentUpstreamTransport(target string, authEnabled bool, tlsEnabled bool, serverName string) agentUpstreamTransport {
	return agentUpstreamTransport{
		Configured:  target != "",
		Target:      target,
		AuthEnabled: authEnabled,
		TLSEnabled:  tlsEnabled,
		ServerName:  serverName,
	}
}
