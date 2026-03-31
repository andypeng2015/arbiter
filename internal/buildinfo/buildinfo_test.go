package buildinfo

import "testing"

func TestCheckOperatorAcceptsMatchingIdentity(t *testing.T) {
	report := CheckOperator(Current())
	if !report.Available || !report.Compatible {
		t.Fatalf("unexpected compatibility report: %+v", report)
	}
}

func TestCheckOperatorRejectsMismatch(t *testing.T) {
	report := CheckOperator(OperatorInfo{
		Product:                 Product,
		BuildVersion:            "1.4.0",
		OperatorContractVersion: "operator.v999",
	})
	if !report.Available || report.Compatible {
		t.Fatalf("unexpected mismatch report: %+v", report)
	}
	if report.Reason != "unexpected operator contract version" {
		t.Fatalf("unexpected mismatch reason: %+v", report)
	}
}
