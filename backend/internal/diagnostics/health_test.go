package diagnostics

import "testing"

func TestBuildHealthSnapshotIncludesPortAndDiscoveryStatus(t *testing.T) {
	snap := BuildHealthSnapshot(true, 19090, "broadcast-ok")
	if snap["agentPort"] != 19090 {
		t.Fatalf("unexpected port: %#v", snap)
	}
	if snap["discovery"] != "broadcast-ok" {
		t.Fatalf("unexpected discovery status: %#v", snap)
	}
}
