package diagnostics

import "testing"

func TestBuildReportIncludesPortsAndLastError(t *testing.T) {
	report := BuildReport(19090, 19091, "firewall-blocked")

	if report.AgentTCPPort != 19090 || report.DiscoveryUDPPort != 19091 {
		t.Fatalf("unexpected report: %#v", report)
	}

	if report.LastConnectionError != "firewall-blocked" {
		t.Fatalf("unexpected report: %#v", report)
	}
}
