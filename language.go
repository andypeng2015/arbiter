package arbiter

import (
	_ "embed"
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

//go:generate go run ./cmd/arbiter-grammar

// grammarBlob is the pre-generated parse table, regenerated from
// ArbiterGrammar() by `go generate ./...` (see cmd/arbiter-grammar). Loading it
// at runtime is ~75x faster than rebuilding the table from the grammar DSL.
// TestGrammarBinIsCurrent guards it against drift.
//
//go:embed grammar.bin
var grammarBlob []byte

var (
	arbLangOnce   sync.Once
	arbLangCached *gotreesitter.Language
	arbLangErr    error
)

func getArbiterLanguage() (*gotreesitter.Language, error) {
	arbLangOnce.Do(func() {
		// Prefer the pre-generated, version-pinned parse table. Fall back to
		// building it from the grammar DSL if the embedded blob is missing or
		// unreadable (e.g. a build that stripped embeds).
		if len(grammarBlob) > 0 {
			if lang, err := gotreesitter.LoadLanguage(grammarBlob); err == nil {
				arbLangCached = lang
				return
			}
		}
		arbLangCached, arbLangErr = GenerateLanguage(ArbiterGrammar())
	})
	return arbLangCached, arbLangErr
}

// GetLanguage returns the compiled arbiter tree-sitter language.
// It is safe for concurrent use (internally cached).
func GetLanguage() (*gotreesitter.Language, error) {
	return getArbiterLanguage()
}
