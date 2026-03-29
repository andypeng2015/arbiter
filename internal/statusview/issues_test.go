package statusview

import "testing"

func TestDefinitionsUseUniqueCodes(t *testing.T) {
	seen := make(map[Code]Definition)
	for _, item := range Definitions() {
		if prior, ok := seen[item.Code]; ok {
			t.Fatalf("duplicate issue code %q: %+v and %+v", item.Code, prior, item)
		}
		seen[item.Code] = item
		if item.Code == "" || item.Scope == "" || item.Severity == "" || item.Description == "" {
			t.Fatalf("incomplete issue definition: %+v", item)
		}
	}
}

func TestNewUsesDefinitionMetadata(t *testing.T) {
	issue := New(CodeAuditUnhealthy, "/tmp/decisions.jsonl", "audit unhealthy: disk full")
	if issue.Code != CodeAuditUnhealthy || issue.Scope != ScopeAudit || issue.Severity != SeverityError || !issue.Blocking {
		t.Fatalf("unexpected issue metadata: %+v", issue)
	}
	if issue.Subject != "/tmp/decisions.jsonl" || issue.Message != "audit unhealthy: disk full" {
		t.Fatalf("unexpected issue payload: %+v", issue)
	}
}
