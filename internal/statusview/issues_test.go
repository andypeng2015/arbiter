package statusview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/arbiter/internal/buildinfo"
)

func TestDefinitionsUseUniqueCodes(t *testing.T) {
	seen := make(map[Code]Definition)
	for _, item := range Definitions() {
		if prior, ok := seen[item.Code]; ok {
			t.Fatalf("duplicate issue code %q: %+v and %+v", item.Code, prior, item)
		}
		seen[item.Code] = item
		if item.Code == "" || item.Scope == "" || item.Severity == "" || item.Description == "" || len(item.Surfaces) == 0 {
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

func TestDefinitionsForSurfaceFiltersCatalog(t *testing.T) {
	items := DefinitionsForSurface(SurfaceRuntime)
	if len(items) == 0 {
		t.Fatal("expected runtime definitions")
	}
	for _, item := range items {
		found := false
		for _, surface := range item.Surfaces {
			if surface == SurfaceRuntime {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("definition %q missing runtime surface: %+v", item.Code, item)
		}
	}
}

func TestProtoDefinitionsExposeSurfaces(t *testing.T) {
	items := ProtoDefinitions()
	if len(items) == 0 {
		t.Fatal("expected proto definitions")
	}
	if items[0].GetCode() == "" || len(items[0].GetSurfaces()) == 0 {
		t.Fatalf("unexpected proto definition payload: %+v", items[0])
	}
}

func TestCatalogForSurfaceCarriesOperatorIdentity(t *testing.T) {
	catalog := CatalogForSurface(SurfaceControl)
	if catalog.Surface != SurfaceControl {
		t.Fatalf("catalog surface = %q, want control", catalog.Surface)
	}
	if catalog.Operator.Product != buildinfo.Product || catalog.Operator.BuildVersion != buildinfo.Version || catalog.Operator.OperatorContractVersion != buildinfo.OperatorContractVersion {
		t.Fatalf("unexpected operator identity: %+v", catalog.Operator)
	}
	if len(catalog.Definitions) == 0 {
		t.Fatal("expected scoped definitions")
	}
	for _, item := range catalog.Definitions {
		found := false
		for _, surface := range item.Surfaces {
			if surface == SurfaceControl {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("definition %q missing control surface: %+v", item.Code, item)
		}
	}
}

func TestProtoCatalogCarriesScopedDefinitionsAndOperatorIdentity(t *testing.T) {
	catalog := ProtoCatalog(SurfaceAgent)
	if catalog.GetSurface() != string(SurfaceAgent) {
		t.Fatalf("catalog surface = %q, want agent", catalog.GetSurface())
	}
	if catalog.GetOperator().GetProduct() != buildinfo.Product || catalog.GetOperator().GetBuildVersion() != buildinfo.Version || catalog.GetOperator().GetOperatorContractVersion() != buildinfo.OperatorContractVersion {
		t.Fatalf("unexpected operator identity: %+v", catalog.GetOperator())
	}
	if len(catalog.GetDefinitions()) == 0 {
		t.Fatal("expected scoped proto definitions")
	}
	for _, item := range catalog.GetDefinitions() {
		found := false
		for _, surface := range item.GetSurfaces() {
			if surface == string(SurfaceAgent) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("definition %q missing agent surface: %+v", item.GetCode(), item)
		}
	}
}

func TestStatusIssueDocsCoverAllCodes(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "docs", "status-issues.md"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read docs/status-issues.md: %v", err)
	}
	text := string(data)
	for _, item := range Definitions() {
		needle := "`" + string(item.Code) + "`"
		if !strings.Contains(text, needle) {
			t.Fatalf("status issue docs missing code %q", item.Code)
		}
	}
}
