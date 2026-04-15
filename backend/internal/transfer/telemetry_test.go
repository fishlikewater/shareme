package transfer

import (
	"testing"
	"time"
)

func TestTelemetrySnapshotComputesProgressRateAndEta(t *testing.T) {
	now := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	telemetry := NewTelemetry("transfer-1", 100)
	telemetry.Start(now)
	telemetry.Advance(40, now.Add(2*time.Second))

	snapshot := telemetry.Snapshot(now.Add(2 * time.Second))
	if snapshot.BytesTransferred != 40 {
		t.Fatalf("expected 40 transferred bytes, got %#v", snapshot)
	}
	if snapshot.ProgressPercent != 40 {
		t.Fatalf("expected 40 percent, got %#v", snapshot)
	}
	if snapshot.RateBytesPerSec <= 0 {
		t.Fatalf("expected positive transfer rate, got %#v", snapshot)
	}
	if snapshot.EtaSeconds == nil || *snapshot.EtaSeconds != 3 {
		t.Fatalf("expected eta to be 3 seconds, got %#v", snapshot)
	}
}

func TestProgressEventGateAllowsFirstEventImmediately(t *testing.T) {
	now := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	gate := NewProgressEventGate(2*time.Second, 128)

	if !gate.Allow(1, now) {
		t.Fatal("expected first progress event to pass immediately")
	}
}

func TestProgressEventGateAllowsWhenPendingBytesReachThreshold(t *testing.T) {
	now := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	gate := NewProgressEventGate(2*time.Second, 128)

	if !gate.Allow(16, now) {
		t.Fatal("expected first progress event to pass immediately")
	}
	if gate.Allow(32, now.Add(200*time.Millisecond)) {
		t.Fatal("expected progress event below byte threshold to remain blocked")
	}
	if !gate.Allow(96, now.Add(400*time.Millisecond)) {
		t.Fatal("expected progress event to pass after accumulated bytes reach threshold")
	}
}

func TestProgressEventGateAllowsWhenIntervalElapses(t *testing.T) {
	now := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	gate := NewProgressEventGate(2*time.Second, 128)

	if !gate.Allow(16, now) {
		t.Fatal("expected first progress event to pass immediately")
	}
	if gate.Allow(16, now.Add(500*time.Millisecond)) {
		t.Fatal("expected progress event inside throttle window to remain blocked")
	}
	if !gate.Allow(16, now.Add(2*time.Second)) {
		t.Fatal("expected elapsed interval to release pending progress event")
	}
}

func TestProgressEventGateFinishForcesPendingEvent(t *testing.T) {
	now := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	gate := NewProgressEventGate(2*time.Second, 128)

	if !gate.Allow(16, now) {
		t.Fatal("expected first progress event to pass immediately")
	}
	if gate.Allow(16, now.Add(500*time.Millisecond)) {
		t.Fatal("expected pending progress event to remain blocked before finish")
	}
	if !gate.Finish(now.Add(700 * time.Millisecond)) {
		t.Fatal("expected finish to flush pending progress event")
	}
	if gate.Finish(now.Add(time.Second)) {
		t.Fatal("expected finish without pending progress to stay silent")
	}
}

func TestTelemetrySnapshotResetsRateAndEtaAfterTransferStalls(t *testing.T) {
	now := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	telemetry := NewTelemetry("transfer-stall", 100)
	telemetry.Start(now)
	telemetry.Advance(40, now.Add(time.Second))

	snapshot := telemetry.Snapshot(now.Add(5 * time.Second))
	if snapshot.RateBytesPerSec != 0 {
		t.Fatalf("expected stalled transfer rate to decay to zero, got %#v", snapshot)
	}
	if snapshot.EtaSeconds != nil {
		t.Fatalf("expected stalled transfer eta to be cleared, got %#v", snapshot)
	}
}
