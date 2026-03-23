package bundle

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/odvcencio/arbiter/compiler"
)

// Metadata trailer layout (appended after the bundle body, before any
// signature trailer):
//
//	[N bytes]  JSON-encoded BundleMetadata
//	[4 bytes]  uint32 little-endian length of the JSON (N)
//	[4 bytes]  magic "ARBM"
//
// Both the metadata and signature trailers are optional and are detected
// by their magic bytes at the end of the file.

var metaMagic = [4]byte{'A', 'R', 'B', 'M'}

const metaTrailerOverhead = 4 + 4 // length (4) + magic (4)

// BundleMetadata holds optional metadata embedded in a bundle.
type BundleMetadata struct {
	CompilerVersion    string    `json:"compiler_version,omitempty"`
	ConformanceProfile string    `json:"conformance_profile,omitempty"`
	CreatedAt          time.Time `json:"created_at,omitempty"`
}

// MarshalWithMetadata serializes a CompiledRuleset with obfuscation and
// appends a JSON metadata trailer after the bundle body.
func MarshalWithMetadata(rs *compiler.CompiledRuleset, opts ObfuscateOptions, meta BundleMetadata) ([]byte, error) {
	body, err := MarshalObfuscated(rs, opts)
	if err != nil {
		return nil, err
	}
	return AppendMetadata(body, meta)
}

// AppendMetadata appends a metadata trailer to raw bundle bytes.
func AppendMetadata(bundleData []byte, meta BundleMetadata) ([]byte, error) {
	js, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal bundle metadata: %w", err)
	}

	out := make([]byte, len(bundleData)+len(js)+metaTrailerOverhead)
	copy(out, bundleData)
	off := len(bundleData)
	copy(out[off:], js)
	off += len(js)
	binary.LittleEndian.PutUint32(out[off:], uint32(len(js)))
	off += 4
	copy(out[off:], metaMagic[:])
	return out, nil
}

// StripMetadata checks whether data ends with an ARBM metadata trailer.
// If found it returns the data without the trailer and the parsed metadata.
// If no trailer is present it returns the original data and a zero-value
// BundleMetadata (no error).
func StripMetadata(data []byte) ([]byte, BundleMetadata, error) {
	var meta BundleMetadata
	if len(data) < metaTrailerOverhead {
		return data, meta, nil
	}

	// Check magic.
	magicOff := len(data) - 4
	var m [4]byte
	copy(m[:], data[magicOff:])
	if m != metaMagic {
		return data, meta, nil
	}

	jsonLen := binary.LittleEndian.Uint32(data[magicOff-4 : magicOff])
	needed := int(jsonLen) + metaTrailerOverhead
	if needed > len(data) {
		return nil, meta, errors.New("metadata trailer length exceeds data size")
	}

	jsonStart := len(data) - needed
	if err := json.Unmarshal(data[jsonStart:jsonStart+int(jsonLen)], &meta); err != nil {
		return nil, meta, fmt.Errorf("unmarshal bundle metadata: %w", err)
	}

	return data[:jsonStart], meta, nil
}

// UnmarshalWithMetadata deserializes a bundle that may have optional metadata
// and/or signature trailers. It strips trailers in reverse order (signature
// first, then metadata) and returns the compiled ruleset plus any metadata.
// Bundles without trailers are handled identically to plain Unmarshal.
func UnmarshalWithMetadata(data []byte) (*compiler.CompiledRuleset, BundleMetadata, error) {
	// Strip signature trailer if present (outermost layer).
	inner := data
	if hasSignatureTrailer(data) {
		inner = data[:len(data)-signTrailerSize]
	}

	// Strip metadata trailer if present.
	body, meta, err := StripMetadata(inner)
	if err != nil {
		return nil, meta, err
	}

	rs, err := Unmarshal(body)
	return rs, meta, err
}

// hasSignatureTrailer reports whether data ends with the "ARBS" magic.
func hasSignatureTrailer(data []byte) bool {
	if len(data) < signTrailerSize {
		return false
	}
	var m [4]byte
	copy(m[:], data[len(data)-4:])
	return m == signMagic
}
