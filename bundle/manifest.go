package bundle

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Manifest is the detached signing sidecar for a bundle (written as
// "<bundle>.sig"). It leaves the ARB1 bundle bytes untouched and travels
// alongside them, giving a signed, versioned, verifiable decision artifact.
type Manifest struct {
	Algorithm       string `json:"algorithm"`                 // "ed25519"
	Signer          string `json:"signer,omitempty"`          // identity hint
	CompilerVersion string `json:"compiler_version,omitempty"`
	Revision        string `json:"revision,omitempty"`
	SourceSHA256    string `json:"source_sha256"` // hex SHA-256 of the bundle bytes
	Signature       string `json:"signature"`     // base64 Ed25519 signature over the bundle bytes
}

// SignManifest produces a sidecar manifest that signs the bundle blob with priv.
func SignManifest(blob []byte, priv ed25519.PrivateKey, signer, compilerVersion string) Manifest {
	sum := sha256.Sum256(blob)
	sig := ed25519.Sign(priv, blob)
	return Manifest{
		Algorithm:       "ed25519",
		Signer:          signer,
		CompilerVersion: compilerVersion,
		SourceSHA256:    hex.EncodeToString(sum[:]),
		Signature:       base64.StdEncoding.EncodeToString(sig),
	}
}

// VerifyBlob checks that blob matches the manifest's hash and signature.
func (m Manifest) VerifyBlob(blob []byte, pub ed25519.PublicKey) error {
	if m.Algorithm != "ed25519" {
		return fmt.Errorf("unsupported signature algorithm %q", m.Algorithm)
	}
	sum := sha256.Sum256(blob)
	if hex.EncodeToString(sum[:]) != m.SourceSHA256 {
		return fmt.Errorf("bundle hash mismatch: artifact does not match its signature manifest")
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid ed25519 public key size %d", len(pub))
	}
	if !ed25519.Verify(pub, blob, sig) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// MarshalSidecar serializes the manifest as the "<bundle>.sig" payload.
func (m Manifest) MarshalSidecar() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// ParseSidecar parses a "<bundle>.sig" payload into a Manifest.
func ParseSidecar(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse signature manifest: %w", err)
	}
	return m, nil
}
