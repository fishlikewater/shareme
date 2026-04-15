package discovery

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestAnnouncementRoundTripPreservesDeviceNameAndPort(t *testing.T) {
	src := Announcement{
		ProtocolVersion: "1",
		DeviceID:        "dev-1",
		DeviceName:      "office-pc",
		AgentTCPPort:    19090,
	}

	data, err := Encode(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.DeviceName != src.DeviceName || got.AgentTCPPort != src.AgentTCPPort {
		t.Fatalf("unexpected decode result: %#v", got)
	}
}

func TestRunnerBroadcastOnceSendsAnnouncement(t *testing.T) {
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected listen error: %v", err)
	}
	defer listener.Close()

	runner := NewRunner(RunnerOptions{
		BroadcastAddr: listener.LocalAddr().String(),
		LocalAnnouncement: Announcement{
			ProtocolVersion: "1",
			DeviceID:        "local-1",
			DeviceName:      "我的电脑",
			AgentTCPPort:    19090,
		},
	})
	defer runner.Close()

	if err := runner.BroadcastOnce(); err != nil {
		t.Fatalf("unexpected broadcast error: %v", err)
	}

	buffer := make([]byte, 1024)
	if err := listener.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("unexpected deadline error: %v", err)
	}

	size, _, err := listener.ReadFrom(buffer)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}

	got, err := Decode(buffer[:size])
	if err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}
	if got.DeviceID != "local-1" || got.DeviceName != "我的电脑" {
		t.Fatalf("unexpected announcement: %#v", got)
	}
}

func TestRunnerStartReceivesAnnouncements(t *testing.T) {
	received := make(chan Announcement, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := NewRunner(RunnerOptions{
		ListenAddr: ":0",
		Interval:   time.Hour,
		LocalAnnouncement: Announcement{
			ProtocolVersion: "1",
			DeviceID:        "local-1",
			DeviceName:      "我的电脑",
			AgentTCPPort:    19090,
		},
		OnAnnouncement: func(announcement Announcement, _ string, _ time.Time) {
			received <- announcement
		},
	})
	defer runner.Close()

	if err := runner.Start(ctx); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	conn, err := net.Dial("udp4", runner.ListenAddr())
	if err != nil {
		t.Fatalf("unexpected dial error: %v", err)
	}
	defer conn.Close()

	payload, err := Encode(Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-1",
		DeviceName:      "会议室电脑",
		AgentTCPPort:    19090,
	})
	if err != nil {
		t.Fatalf("unexpected encode error: %v", err)
	}

	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	select {
	case announcement := <-received:
		if announcement.DeviceID != "peer-1" {
			t.Fatalf("unexpected announcement: %#v", announcement)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected announcement callback")
	}
}

func TestRegistryUpsertUsesAnnouncementPortAsPeerEndpoint(t *testing.T) {
	registry := NewRegistry()

	record := registry.Upsert(Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-1",
		DeviceName:      "meeting-room",
		AgentTCPPort:    52351,
	}, "192.168.1.20:61123", time.Date(2026, 4, 12, 8, 0, 0, 0, time.UTC))

	if record.LastKnownAddr != "192.168.1.20:52351" {
		t.Fatalf("expected peer endpoint to use announced tcp port, got %#v", record)
	}
}

func TestRegistryListMarksExpiredPeerOffline(t *testing.T) {
	registry := NewRegistry()
	now := time.Now().UTC()

	registry.Upsert(Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-old",
		DeviceName:      "old-peer",
		AgentTCPPort:    52351,
	}, "192.168.1.20:61123", now.Add(-10*time.Second))
	registry.Upsert(Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-fresh",
		DeviceName:      "fresh-peer",
		AgentTCPPort:    52352,
	}, "192.168.1.21:61124", now)

	records := registry.List()
	if len(records) != 2 {
		t.Fatalf("expected two peers, got %#v", records)
	}

	byID := make(map[string]PeerRecord, len(records))
	for _, record := range records {
		byID[record.DeviceID] = record
	}
	if byID["peer-old"].Online || byID["peer-old"].Reachable {
		t.Fatalf("expected expired peer to be offline, got %#v", byID["peer-old"])
	}
	if !byID["peer-fresh"].Online || !byID["peer-fresh"].Reachable {
		t.Fatalf("expected fresh peer to stay reachable, got %#v", byID["peer-fresh"])
	}
}

func TestRegistryMarkDirectActiveKeepsPeerReachableWithoutFreshBroadcast(t *testing.T) {
	registry := NewRegistry()
	now := time.Now().UTC()

	registry.Upsert(Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-1",
		DeviceName:      "peer-one",
		AgentTCPPort:    19090,
	}, "192.168.1.20:52351", now.Add(-10*time.Second))

	registry.MarkDirectActive("peer-1", "", 0, now)

	records := registry.List()
	if len(records) != 1 {
		t.Fatalf("expected one peer, got %#v", records)
	}
	if records[0].Online {
		t.Fatalf("expected peer to be offline after discovery ttl, got %#v", records[0])
	}
	if !records[0].Reachable {
		t.Fatalf("expected direct activity to keep peer reachable, got %#v", records[0])
	}
}

func TestRegistryKeepsRecoveredPeerReachablePastDiscoveryTTL(t *testing.T) {
	registry := NewRegistry()
	now := time.Now().UTC()

	registry.Upsert(Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-3",
		DeviceName:      "peer-three",
		AgentTCPPort:    19090,
	}, "192.168.1.30:52351", now.Add(-10*time.Second))
	registry.MarkReachable("peer-3", false)
	registry.MarkDirectActive("peer-3", "", 0, now.Add(-30*time.Second))

	records := registry.List()
	if len(records) != 1 {
		t.Fatalf("expected one peer, got %#v", records)
	}
	if records[0].Online {
		t.Fatalf("expected peer to stay offline, got %#v", records[0])
	}
	if !records[0].Reachable {
		t.Fatalf("expected recovered peer to stay reachable while direct activity is still fresh, got %#v", records[0])
	}
}

func TestRegistryRecoveredPeerReachabilityExpiresAfterDirectTTL(t *testing.T) {
	registry := NewRegistry()
	now := time.Now().UTC()

	registry.Upsert(Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-4",
		DeviceName:      "peer-four",
		AgentTCPPort:    19090,
	}, "192.168.1.40:52351", now.Add(-10*time.Second))
	registry.MarkReachable("peer-4", false)
	registry.MarkDirectActive("peer-4", "", 0, now.Add(-directReachabilityTTL-time.Second))

	records := registry.List()
	if len(records) != 1 {
		t.Fatalf("expected one peer, got %#v", records)
	}
	if records[0].Reachable {
		t.Fatalf("expected recovered peer to expire after direct ttl, got %#v", records[0])
	}
}

func TestRegistryFreshBroadcastDoesNotOverrideDirectFailureState(t *testing.T) {
	registry := NewRegistry()
	now := time.Now().UTC()

	registry.Upsert(Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-5",
		DeviceName:      "peer-five",
		AgentTCPPort:    19090,
	}, "192.168.1.50:52351", now.Add(-2*time.Second))
	registry.MarkDirectActive("peer-5", "192.168.1.50:19090", 19090, now.Add(-time.Second))
	registry.MarkReachable("peer-5", false)

	registry.Upsert(Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-5",
		DeviceName:      "peer-five",
		AgentTCPPort:    19090,
	}, "192.168.1.50:52351", now)

	records := registry.List()
	if len(records) != 1 {
		t.Fatalf("expected one peer, got %#v", records)
	}
	if !records[0].Online {
		t.Fatalf("expected peer to stay online after fresh broadcast, got %#v", records[0])
	}
	if records[0].Reachable {
		t.Fatalf("expected direct failure state to survive broadcast refresh, got %#v", records[0])
	}
}

func TestRegistryMarkDirectActiveNeedsKnownEndpointToReportReachable(t *testing.T) {
	registry := NewRegistry()
	now := time.Now().UTC()

	registry.MarkDirectActive("peer-2", "", 0, now)

	records := registry.List()
	if len(records) != 1 {
		t.Fatalf("expected one peer, got %#v", records)
	}
	if records[0].Reachable {
		t.Fatalf("expected peer without endpoint to stay unreachable, got %#v", records[0])
	}
}
