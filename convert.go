package arbiter

import (
	"m31labs.dev/arbiter/decompile"
)

// ConvertJSON converts Arishem-style JSON condition and action expressions
// into equivalent .arb source text. The returned bytes can be passed directly
// to Compile. It is the permanent bridge for callers migrating from the JSON API.
func ConvertJSON(condJSON, actJSON string) ([]byte, error) {
	rules := []decompile.ArishemRule{
		{
			Name:      "rule0",
			Priority:  0,
			Condition: condJSON,
			Action:    actJSON,
		},
	}
	text, err := decompile.ArishemToArb(rules)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

// ConvertJSONRules converts a slice of Arishem JSON rules into .arb source text.
// The returned bytes can be passed directly to Compile. It is the permanent
// bridge for callers migrating from the JSON API.
func ConvertJSONRules(rules []JSONRule) ([]byte, error) {
	arishemRules := make([]decompile.ArishemRule, len(rules))
	for i, r := range rules {
		arishemRules[i] = decompile.ArishemRule{
			Name:      r.Name,
			Priority:  r.Priority,
			Condition: r.Condition,
			Action:    r.Action,
		}
	}
	text, err := decompile.ArishemToArb(arishemRules)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}
