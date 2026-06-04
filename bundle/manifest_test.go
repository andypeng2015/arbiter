package bundle

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestManifestSignVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	blob := []byte("ARB1 pretend bundle payload")

	m := SignManifest(blob, priv, "identity://acme/ci", "1.7.0")
	if err := m.VerifyBlob(blob, pub); err != nil {
		t.Fatalf("valid signature should verify: %v", err)
	}

	// Tampered artifact must fail (hash mismatch).
	if err := m.VerifyBlob([]byte("tampered payload"), pub); err == nil {
		t.Fatal("tampered blob should fail verification")
	}

	// Wrong key must fail.
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := m.VerifyBlob(blob, otherPub); err == nil {
		t.Fatal("wrong public key should fail verification")
	}
}

func TestManifestSidecarRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	blob := []byte("payload bytes")

	m := SignManifest(blob, priv, "signer", "v1")
	data, err := m.MarshalSidecar()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSidecar(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := parsed.VerifyBlob(blob, pub); err != nil {
		t.Fatalf("round-tripped manifest should verify: %v", err)
	}
	if parsed.Signer != "signer" || parsed.CompilerVersion != "v1" {
		t.Fatalf("manifest fields not preserved: %+v", parsed)
	}
}
