package govern

import "testing"

func TestNestDottedKeys(t *testing.T) {
	nested := NestDottedKeys(map[string]any{
		"user.id":    "u_123",
		"user.plan":  "enterprise",
		"request_id": "req_1",
	})

	user, ok := nested["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested user map, got %#v", nested["user"])
	}
	if user["id"] != "u_123" || user["plan"] != "enterprise" {
		t.Fatalf("unexpected nested user values: %#v", user)
	}
	if nested["request_id"] != "req_1" {
		t.Fatalf("expected request_id passthrough, got %#v", nested["request_id"])
	}
}

func TestNestDottedKeysConflictsPreferNestedStructure(t *testing.T) {
	nested := NestDottedKeys(map[string]any{
		"user":      "scalar",
		"user.name": "alice",
	})

	user, ok := nested["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user map after conflict resolution, got %#v", nested["user"])
	}
	if got := user["name"]; got != "alice" {
		t.Fatalf("user.name = %#v, want alice", got)
	}
}

func TestNestDottedKeysMergesExistingMapAndDottedChild(t *testing.T) {
	nested := NestDottedKeys(map[string]any{
		"user": map[string]any{
			"id": "u_123",
		},
		"user.name": "alice",
	})

	user, ok := nested["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user map, got %#v", nested["user"])
	}
	if got := user["id"]; got != "u_123" {
		t.Fatalf("user.id = %#v, want u_123", got)
	}
	if got := user["name"]; got != "alice" {
		t.Fatalf("user.name = %#v, want alice", got)
	}
}
