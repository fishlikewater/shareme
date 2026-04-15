package transfer

import (
	"testing"
	"time"
)

func TestAdaptiveParallelismStartsWithTwoWorkers(t *testing.T) {
	controller := NewAdaptiveParallelism(2, 2, 8)

	if controller.Current() != 2 {
		t.Fatalf("expected initial parallelism 2, got %d", controller.Current())
	}
}

func TestAdaptiveParallelismIncreasesWhenThroughputGrows(t *testing.T) {
	controller := NewAdaptiveParallelism(2, 2, 8)

	controller.Observe(WindowMetrics{BytesCommitted: 100, Duration: time.Second})
	next := controller.Observe(WindowMetrics{BytesCommitted: 120, Duration: time.Second})
	if next != 4 {
		t.Fatalf("expected controller to scale up aggressively to 4, got %d", next)
	}
}

func TestAdaptiveParallelismFallsBackWhenRetriesSpike(t *testing.T) {
	controller := NewAdaptiveParallelism(3, 2, 8)

	controller.Observe(WindowMetrics{BytesCommitted: 200, Duration: time.Second, RetryCount: 0})
	next := controller.Observe(WindowMetrics{BytesCommitted: 220, Duration: time.Second, RetryCount: 2})
	if next != 2 {
		t.Fatalf("expected controller to fall back to 2, got %d", next)
	}
}

func TestAdaptiveParallelismKeepsCurrentOnEmptyWindowWithoutRetries(t *testing.T) {
	controller := NewAdaptiveParallelism(4, 2, 8)

	controller.Observe(WindowMetrics{BytesCommitted: 200, Duration: time.Second, RetryCount: 0})
	next := controller.Observe(WindowMetrics{BytesCommitted: 0, Duration: time.Second, RetryCount: 0})
	if next != 4 {
		t.Fatalf("expected controller to keep current parallelism on empty window, got %d", next)
	}
}

func TestAdaptiveParallelismDoesNotInflateAfterEmptyWindow(t *testing.T) {
	controller := NewAdaptiveParallelism(4, 2, 8)

	controller.Observe(WindowMetrics{BytesCommitted: 200, Duration: time.Second, RetryCount: 0})
	controller.Observe(WindowMetrics{BytesCommitted: 0, Duration: time.Second, RetryCount: 0})
	next := controller.Observe(WindowMetrics{BytesCommitted: 180, Duration: time.Second, RetryCount: 0})
	if next != 4 {
		t.Fatalf("expected controller to keep stable parallelism after empty window, got %d", next)
	}
}
