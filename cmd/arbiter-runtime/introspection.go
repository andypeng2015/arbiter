package main

import "crypto/tls"

type runtimeControlTransport struct {
	Enabled          bool   `json:"enabled"`
	Address          string `json:"address,omitempty"`
	PublicListener   bool   `json:"public_listener,omitempty"`
	AuthEnabled      bool   `json:"auth_enabled"`
	TLSEnabled       bool   `json:"tls_enabled"`
	MutualTLSEnabled bool   `json:"mutual_tls_enabled"`
}

type runtimeCapabilityTransport struct {
	Configured  bool   `json:"configured"`
	Target      string `json:"target,omitempty"`
	AuthEnabled bool   `json:"auth_enabled"`
	TLSEnabled  bool   `json:"tls_enabled"`
	ServerName  string `json:"server_name,omitempty"`
}

func newRuntimeControlTransport(address string, tokens []string, tlsConfig *tls.Config, publicListener bool) runtimeControlTransport {
	status := runtimeControlTransport{
		Enabled:        address != "",
		Address:        address,
		PublicListener: publicListener,
		AuthEnabled:    len(tokens) > 0,
		TLSEnabled:     tlsConfig != nil,
	}
	if tlsConfig != nil && tlsConfig.ClientAuth == tls.RequireAndVerifyClientCert {
		status.MutualTLSEnabled = true
	}
	return status
}

func newRuntimeCapabilityTransport(target string, authEnabled bool, tlsEnabled bool, serverName string) runtimeCapabilityTransport {
	return runtimeCapabilityTransport{
		Configured:  target != "",
		Target:      target,
		AuthEnabled: authEnabled,
		TLSEnabled:  tlsEnabled,
		ServerName:  serverName,
	}
}
