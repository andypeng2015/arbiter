package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleSignVerifyCLI(t *testing.T) {
	dir := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "k.key")
	if err := os.WriteFile(keyPath, priv, 0o600); err != nil {
		t.Fatal(err)
	}
	pubPath := filepath.Join(dir, "k.pub")
	if err := os.WriteFile(pubPath, pub, 0o644); err != nil {
		t.Fatal(err)
	}
	arbPath := filepath.Join(dir, "r.arb")
	if err := os.WriteFile(arbPath, []byte("rule R { when { score > 1 } then A {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "r.arbb")

	if err := runBundle([]string{arbPath, "-o", outPath, "--force", "--sign", keyPath}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	// The signature is a detached sidecar; the ARB1 bundle stays pristine.
	if _, err := os.Stat(outPath + ".sig"); err != nil {
		t.Fatalf("expected detached sidecar %s.sig: %v", outPath, err)
	}
	if err := runBundle([]string{"--verify", outPath, "--pub", pubPath}); err != nil {
		t.Fatalf("verify of a validly-signed bundle: %v", err)
	}

	// Tampering the bundle must make verification fail.
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-1] ^= 0xff
	if err := os.WriteFile(outPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runBundle([]string{"--verify", outPath, "--pub", pubPath}); err == nil {
		t.Fatal("verification must fail on a tampered bundle")
	}
}
