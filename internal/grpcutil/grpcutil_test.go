package grpcutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestNormalizeTarget(t *testing.T) {
	tests := []struct {
		name          string
		target        string
		forceInsecure bool
		tlsHint       bool
		wantTarget    string
		wantSecure    bool
		wantErr       bool
	}{
		{
			name:       "plaintext scheme",
			target:     "grpc://127.0.0.1:7081",
			wantTarget: "127.0.0.1:7081",
		},
		{
			name:       "secure scheme",
			target:     "grpcs://arbiter.internal:7443",
			wantTarget: "arbiter.internal:7443",
			wantSecure: true,
		},
		{
			name:       "tls hint on bare target",
			target:     "arbiter.internal:7443",
			tlsHint:    true,
			wantTarget: "arbiter.internal:7443",
			wantSecure: true,
		},
		{
			name:          "conflicting secure and plaintext",
			target:        "https://arbiter.internal:7443",
			forceInsecure: true,
			wantErr:       true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTarget, gotSecure, err := NormalizeTarget(tc.target, tc.forceInsecure, tc.tlsHint)
			if tc.wantErr {
				if err == nil {
					t.Fatal("NormalizeTarget: expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeTarget: %v", err)
			}
			if gotTarget != tc.wantTarget || gotSecure != tc.wantSecure {
				t.Fatalf("NormalizeTarget(%q) = (%q, %v), want (%q, %v)", tc.target, gotTarget, gotSecure, tc.wantTarget, tc.wantSecure)
			}
		})
	}
}

func TestLoadAuthTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.txt")
	if err := os.WriteFile(path, []byte("alpha,beta\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := LoadAuthTokens([]string{"alpha", " delta "}, path)
	if err != nil {
		t.Fatalf("LoadAuthTokens: %v", err)
	}
	want := []string{"alpha", "delta", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("LoadAuthTokens len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LoadAuthTokens[%d] = %q, want %q (%v)", i, got[i], want[i], got)
		}
	}
}

func TestWithBearerToken(t *testing.T) {
	ctx := WithBearerToken(context.Background(), "top-secret")
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected outgoing metadata")
	}
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer top-secret" {
		t.Fatalf("authorization metadata = %v, want Bearer top-secret", got)
	}
}
