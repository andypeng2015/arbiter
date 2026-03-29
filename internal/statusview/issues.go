package statusview

import (
	"strings"

	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
)

type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Issue is one canonical operator-facing problem surfaced by runtime, agent,
// and hosted control status endpoints.
type Issue struct {
	Severity Severity `json:"severity"`
	Scope    string   `json:"scope"`
	Subject  string   `json:"subject,omitempty"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Blocking bool     `json:"blocking,omitempty"`
}

func Error(scope, subject, code, message string, blocking bool) Issue {
	return Issue{
		Severity: SeverityError,
		Scope:    strings.TrimSpace(scope),
		Subject:  strings.TrimSpace(subject),
		Code:     strings.TrimSpace(code),
		Message:  strings.TrimSpace(message),
		Blocking: blocking,
	}
}

func Warning(scope, subject, code, message string) Issue {
	return Issue{
		Severity: SeverityWarning,
		Scope:    strings.TrimSpace(scope),
		Subject:  strings.TrimSpace(subject),
		Code:     strings.TrimSpace(code),
		Message:  strings.TrimSpace(message),
	}
}

func ProtoIssues(items []Issue) []*arbiterv1.StatusIssue {
	if len(items) == 0 {
		return nil
	}
	out := make([]*arbiterv1.StatusIssue, 0, len(items))
	for _, item := range items {
		out = append(out, &arbiterv1.StatusIssue{
			Severity: string(item.Severity),
			Scope:    item.Scope,
			Subject:  item.Subject,
			Code:     item.Code,
			Message:  item.Message,
			Blocking: item.Blocking,
		})
	}
	return out
}
