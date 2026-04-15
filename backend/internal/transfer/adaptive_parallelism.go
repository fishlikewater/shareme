package transfer

import "time"

type WindowMetrics struct {
	BytesCommitted int64
	Duration       time.Duration
	RetryCount     int
}

func (w WindowMetrics) Throughput() float64 {
	if w.Duration <= 0 {
		return 0
	}
	return float64(w.BytesCommitted) / w.Duration.Seconds()
}

type AdaptiveParallelism struct {
	current     int
	min         int
	max         int
	initialized bool
	previous    WindowMetrics
	declines    int
}

func NewAdaptiveParallelism(initial int, min int, max int) *AdaptiveParallelism {
	if min <= 0 {
		min = 1
	}
	if max < min {
		max = min
	}
	if initial < min {
		initial = min
	}
	if initial > max {
		initial = max
	}
	return &AdaptiveParallelism{
		current: initial,
		min:     min,
		max:     max,
	}
}

func (a *AdaptiveParallelism) Current() int {
	return a.current
}

func (a *AdaptiveParallelism) Observe(window WindowMetrics) int {
	if !a.initialized {
		a.initialized = true
		a.previous = window
		return a.current
	}

	previousThroughput := a.previous.Throughput()
	currentThroughput := window.Throughput()
	if currentThroughput <= 0 {
		a.declines = 0
		return a.current
	}
	switch {
	case window.RetryCount > a.previous.RetryCount:
		a.current -= 2
		a.declines = 0
	case currentThroughput <= 0:
		// 分片完成是脉冲式的，空窗口不视为退化，避免并发抖动。
	case previousThroughput <= 0:
		a.current += 2
		a.declines = 0
	default:
		gain := (currentThroughput - previousThroughput) / previousThroughput
		switch {
		case gain > 0.15:
			if a.current < 8 {
				a.current += 2
			} else {
				a.current++
			}
			a.declines = 0
		case gain > 0.05:
			a.current++
			a.declines = 0
		case gain < -0.2:
			a.declines++
			if a.declines >= 2 {
				a.current--
				a.declines = 0
			}
		default:
			a.declines = 0
		}
	}

	if a.current < a.min {
		a.current = a.min
	}
	if a.current > a.max {
		a.current = a.max
	}
	a.previous = window
	return a.current
}
