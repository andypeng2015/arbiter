package govern

import (
	"sort"
	"strings"
)

// NestDottedKeys converts a flat map with dotted keys into nested maps.
func NestDottedKeys(flat map[string]any) map[string]any {
	result := make(map[string]any)
	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		leftDepth := strings.Count(keys[i], ".")
		rightDepth := strings.Count(keys[j], ".")
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return keys[i] < keys[j]
	})

	for _, k := range keys {
		v := flat[k]
		parts := strings.Split(k, ".")
		if len(parts) == 1 {
			result[k] = v
			continue
		}
		current := result
		for i, part := range parts {
			if i == len(parts)-1 {
				current[part] = v
				continue
			}
			next, ok := current[part].(map[string]any)
			if !ok {
				next = make(map[string]any)
				current[part] = next
			}
			current = next
		}
	}
	return result
}
