package bundle_test

import (
	"crypto/ed25519"
	"testing"

	"m31labs.dev/arbiter/bundle"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := bundle.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	original := []byte("ARB1 fake bundle payload for testing")

	signed, err := bundle.Sign(original, priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Signed data should be 68 bytes longer (64 sig + 4 magic).
	if len(signed) != len(original)+68 {
		t.Fatalf("expected signed length %d, got %d", len(original)+68, len(signed))
	}

	recovered, err := bundle.Verify(signed, pub)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if string(recovered) != string(original) {
		t.Fatalf("recovered payload mismatch")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	_, priv, err := bundle.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	otherPub, _, err := bundle.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate other key pair: %v", err)
	}

	signed, err := bundle.Sign([]byte("payload"), priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = bundle.Verify(signed, otherPub)
	if err == nil {
		t.Fatal("expected verify to fail with wrong key")
	}
}

func TestVerifyTamperedData(t *testing.T) {
	pub, priv, err := bundle.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	signed, err := bundle.Sign([]byte("original payload"), priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Tamper with the payload portion (first byte).
	signed[0] ^= 0xFF

	_, err = bundle.Verify(signed, pub)
	if err == nil {
		t.Fatal("expected verify to fail on tampered data")
	}
}

func TestVerifyUnsignedBundle(t *testing.T) {
	pub := make(ed25519.PublicKey, ed25519.PublicKeySize)

	// A plain bundle with no signature trailer.
	unsigned := []byte("ARB1 some bundle content without a signature")

	_, err := bundle.Verify(unsigned, pub)
	if err == nil {
		t.Fatal("expected verify to fail on unsigned bundle")
	}
}
