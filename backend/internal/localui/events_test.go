package localui

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"message-share/backend/internal/api"
)

func TestEventsStreamDeliversBacklogAndLiveEvents(t *testing.T) {
	bus := api.NewEventBus()
	bus.Publish("peer.updated", map[string]any{"deviceId": "peer-1"})

	service := NewService(func() RuntimeCommands { return fakeCommands{} }, bus)
	server := httptest.NewServer(NewServer(ServiceDeps{Service: service}).Handler())
	defer server.Close()

	req, err := newLoopbackRequest(server.URL + "/api/events/stream?afterSeq=0")
	if err != nil {
		t.Fatalf("newLoopbackRequest() error = %v", err)
	}

	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	reader := bufio.NewReader(response.Body)
	first := readSSEEvent(t, reader)
	if first.EventSeq != 1 || first.Kind != "peer.updated" {
		t.Fatalf("unexpected first event: %#v", first)
	}

	bus.Publish("transfer.updated", map[string]any{"transferId": "tx-1"})
	second := readSSEEvent(t, reader)
	if second.EventSeq != 2 || second.Kind != "transfer.updated" {
		t.Fatalf("unexpected second event: %#v", second)
	}
}

func newLoopbackRequest(url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return req, nil
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) api.Event {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("ReadString() error = %v", err)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		var event api.Event
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &event); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		return event
	}

	t.Fatal("timed out waiting for SSE event")
	return api.Event{}
}

func TestEventsStreamResumesFromAfterSeq(t *testing.T) {
	bus := api.NewEventBus()
	bus.Publish("peer.updated", map[string]any{"deviceId": "peer-1"})
	bus.Publish("health.updated", map[string]any{"status": "ok"})

	service := NewService(func() RuntimeCommands { return fakeCommands{} }, bus)
	server := httptest.NewServer(NewServer(ServiceDeps{Service: service}).Handler())
	defer server.Close()

	req, err := newLoopbackRequest(server.URL + "/api/events/stream?afterSeq=1")
	if err != nil {
		t.Fatalf("newLoopbackRequest() error = %v", err)
	}
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	event := readSSEEvent(t, bufio.NewReader(response.Body))
	if event.EventSeq != 2 || event.Kind != "health.updated" {
		t.Fatalf("unexpected resumed event: %#v", event)
	}
}
