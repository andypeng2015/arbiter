package bundle

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
)

// Signature trailer layout (appended to the end of the file):
//
//	[64 bytes] Ed25519 signature over all preceding bytes
//	[4 bytes]  magic "ARBS"
//
// The signature covers every byte before the trailer (i.e. len(data) - 68).

var signMagic = [4]byte{'A', 'R', 'B', 'S'}

const signTrailerSize = ed25519.SignatureSize + 4 // 64 + 4 = 68

// GenerateKeyPair returns a new Ed25519 public/private key pair.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ed25519 key pair: %w", err)
	}
	return pub, priv, nil
}

// Sign appends an Ed25519 signature trailer to bundleData.
// The returned slice is bundleData + signature (64 bytes) + "ARBS" (4 bytes).
func Sign(bundleData []byte, privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid private key size")
	}

	sig := ed25519.Sign(privateKey, bundleData)

	out := make([]byte, len(bundleData)+signTrailerSize)
	copy(out, bundleData)
	copy(out[len(bundleData):], sig)
	copy(out[len(bundleData)+ed25519.SignatureSize:], signMagic[:])
	return out, nil
}

// Verify checks the Ed25519 signature trailer and returns the original bundle
// data (everything before the trailer). Returns an error if the trailer is
// missing or the signature does not verify.
func Verify(signedData []byte, publicKey ed25519.PublicKey) ([]byte, error) {
	if len(signedData) < signTrailerSize {
		return nil, errors.New("data too short to contain signature trailer")
	}

	// Check magic at the very end.
	magicOff := len(signedData) - 4
	var m [4]byte
	copy(m[:], signedData[magicOff:])
	if m != signMagic {
		return nil, errors.New("signature trailer not found (missing ARBS magic)")
	}

	payload := signedData[:len(signedData)-signTrailerSize]
	sig := signedData[len(payload) : len(payload)+ed25519.SignatureSize]

	if !ed25519.Verify(publicKey, payload, sig) {
		return nil, errors.New("signature verification failed")
	}
	return payload, nil
}
