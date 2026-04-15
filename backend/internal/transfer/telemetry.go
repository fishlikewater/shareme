package transfer

import (
	"io"
	"math"
	"sync"
	"time"
)

const (
	telemetrySampleRetention = 4 * time.Second
	telemetryStallThreshold  = 2 * time.Second
)

type Snapshot struct {
	BytesTransferred int64
	ProgressPercent  float64
	RateBytesPerSec  float64
	EtaSeconds       *int64
	StartedAt        time.Time
	UpdatedAt        time.Time
}

type ProgressEventGate struct {
	minInterval time.Duration
	minBytes    int64
	pending     int64
	lastAllowed time.Time
	emitted     bool
	mu          sync.Mutex
}

func NewProgressEventGate(minInterval time.Duration, minBytes int64) *ProgressEventGate {
	return &ProgressEventGate{
		minInterval: minInterval,
		minBytes:    minBytes,
	}
}

func (g *ProgressEventGate) Allow(delta int64, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if delta > 0 {
		g.pending += delta
	}

	return g.allowLocked(now.UTC(), false)
}

func (g *ProgressEventGate) Finish(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.allowLocked(now.UTC(), true)
}

func (g *ProgressEventGate) allowLocked(now time.Time, force bool) bool {
	if !g.emitted {
		return g.markAllowedLocked(now)
	}
	if force && g.pending > 0 {
		return g.markAllowedLocked(now)
	}
	if g.minBytes > 0 && g.pending >= g.minBytes {
		return g.markAllowedLocked(now)
	}
	if g.minInterval > 0 && !now.Before(g.lastAllowed.Add(g.minInterval)) && g.pending > 0 {
		return g.markAllowedLocked(now)
	}
	return false
}

func (g *ProgressEventGate) markAllowedLocked(now time.Time) bool {
	g.emitted = true
	g.lastAllowed = now
	g.pending = 0
	return true
}

type sample struct {
	at    time.Time
	bytes int64
}

type Telemetry struct {
	totalBytes       int64
	bytesTransferred int64
	startedAt        time.Time
	updatedAt        time.Time
	samples          []sample
	mu               sync.Mutex
}

func NewTelemetry(_ string, totalBytes int64) *Telemetry {
	return &Telemetry{totalBytes: totalBytes}
}

func (t *Telemetry) Start(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.startedAt = now.UTC()
	t.updatedAt = now.UTC()
	t.samples = []sample{{at: t.startedAt, bytes: 0}}
}

func (t *Telemetry) Advance(delta int64, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.startedAt.IsZero() {
		t.startedAt = now.UTC()
		t.samples = []sample{{at: t.startedAt, bytes: 0}}
	}
	t.bytesTransferred += delta
	if t.bytesTransferred < 0 {
		t.bytesTransferred = 0
	}
	t.updatedAt = now.UTC()
	t.samples = append(t.samples, sample{at: t.updatedAt, bytes: t.bytesTransferred})
	t.trimSamplesLocked()
}

func (t *Telemetry) Snapshot(now time.Time) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked(now.UTC())
}

func (t *Telemetry) trimSamplesLocked() {
	if len(t.samples) <= 2 {
		return
	}

	cutoff := t.updatedAt.Add(-telemetrySampleRetention)
	index := 0
	for index < len(t.samples)-2 && t.samples[index+1].at.Before(cutoff) {
		index++
	}
	if index > 0 {
		t.samples = append([]sample(nil), t.samples[index:]...)
	}
}

func (t *Telemetry) snapshotLocked(now time.Time) Snapshot {
	if t.startedAt.IsZero() {
		return Snapshot{}
	}

	progress := 0.0
	if t.totalBytes > 0 {
		progress = (float64(t.bytesTransferred) / float64(t.totalBytes)) * 100
		if progress > 100 {
			progress = 100
		}
	}

	var rate float64
	if len(t.samples) >= 2 {
		first := t.samples[0]
		last := t.samples[len(t.samples)-1]
		seconds := last.at.Sub(first.at).Seconds()
		if seconds > 0 {
			rate = float64(last.bytes-first.bytes) / seconds
		}
	}
	if rate == 0 {
		seconds := now.Sub(t.startedAt).Seconds()
		if seconds > 0 {
			rate = float64(t.bytesTransferred) / seconds
		}
	}
	if !t.updatedAt.IsZero() && now.Sub(t.updatedAt) >= telemetryStallThreshold {
		rate = 0
	}

	var eta *int64
	if rate > 0 && t.totalBytes > t.bytesTransferred {
		seconds := int64(math.Ceil(float64(t.totalBytes-t.bytesTransferred) / rate))
		eta = &seconds
	}

	return Snapshot{
		BytesTransferred: t.bytesTransferred,
		ProgressPercent:  progress,
		RateBytesPerSec:  rate,
		EtaSeconds:       eta,
		StartedAt:        t.startedAt,
		UpdatedAt:        t.updatedAt,
	}
}

type Registry struct {
	mu         sync.RWMutex
	telemetry  map[string]*Telemetry
	directions map[string]string
}

func NewRegistry() *Registry {
	return &Registry{
		telemetry:  make(map[string]*Telemetry),
		directions: make(map[string]string),
	}
}

func (r *Registry) Start(transferID string, totalBytes int64, direction string, startedAt time.Time) *Telemetry {
	telemetry := NewTelemetry(transferID, totalBytes)
	telemetry.Start(startedAt)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.telemetry[transferID] = telemetry
	r.directions[transferID] = direction
	return telemetry
}

func (r *Registry) Snapshot(transferID string, now time.Time) (Snapshot, bool) {
	r.mu.RLock()
	telemetry := r.telemetry[transferID]
	r.mu.RUnlock()
	if telemetry == nil {
		return Snapshot{}, false
	}
	return telemetry.Snapshot(now), true
}

func (r *Registry) Finish(transferID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.telemetry, transferID)
	delete(r.directions, transferID)
}

type progressReader struct {
	reader    io.Reader
	onAdvance func(int64)
}

func NewProgressReader(reader io.Reader, onAdvance func(int64)) io.Reader {
	return &progressReader{reader: reader, onAdvance: onAdvance}
}

func (r *progressReader) Read(buffer []byte) (int, error) {
	readBytes, err := r.reader.Read(buffer)
	if readBytes > 0 && r.onAdvance != nil {
		r.onAdvance(int64(readBytes))
	}
	return readBytes, err
}

type progressWriter struct {
	writer    io.Writer
	onAdvance func(int64)
}

func NewProgressWriter(writer io.Writer, onAdvance func(int64)) io.Writer {
	return &progressWriter{writer: writer, onAdvance: onAdvance}
}

func (w *progressWriter) Write(buffer []byte) (int, error) {
	written, err := w.writer.Write(buffer)
	if written > 0 && w.onAdvance != nil {
		w.onAdvance(int64(written))
	}
	return written, err
}
