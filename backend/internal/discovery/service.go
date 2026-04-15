package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Announcement struct {
	ProtocolVersion string `json:"protocolVersion"`
	DeviceID        string `json:"deviceId"`
	DeviceName      string `json:"deviceName"`
	AgentTCPPort    int    `json:"agentTcpPort"`
}

type PeerRecord struct {
	DeviceID               string    `json:"deviceId"`
	DeviceName             string    `json:"deviceName"`
	AgentTCPPort           int       `json:"agentTcpPort"`
	LastKnownAddr          string    `json:"lastKnownAddr,omitempty"`
	PinnedFingerprint      string    `json:"pinnedFingerprint,omitempty"`
	Online                 bool      `json:"online"`
	Reachable              bool      `json:"reachable"`
	Trusted                bool      `json:"trusted"`
	DiscoverySource        string    `json:"discoverySource"`
	LastSeenAt             time.Time `json:"lastSeenAt"`
	LastDirectActiveAt     time.Time `json:"lastDirectActiveAt"`
	ReachabilitySuppressed bool      `json:"-"`
}

const peerTTL = 6 * time.Second
const directReachabilityTTL = 2 * time.Minute

type Registry struct {
	mu    sync.RWMutex
	peers map[string]PeerRecord
}

type RunnerOptions struct {
	ListenAddr        string
	BroadcastAddr     string
	Interval          time.Duration
	LocalAnnouncement Announcement
	OnAnnouncement    func(announcement Announcement, addr string, seenAt time.Time)
}

type Runner struct {
	options       RunnerOptions
	listenConn    net.PacketConn
	broadcastConn net.PacketConn
	once          sync.Once
}

func NewRegistry() *Registry {
	return &Registry{
		peers: make(map[string]PeerRecord),
	}
}

func NewRunner(options RunnerOptions) *Runner {
	if options.ListenAddr == "" {
		options.ListenAddr = ":19091"
	}
	if options.Interval <= 0 {
		options.Interval = 2 * time.Second
	}

	return &Runner{options: options}
}

func Encode(a Announcement) ([]byte, error) {
	return json.Marshal(a)
}

func Decode(data []byte) (Announcement, error) {
	var announcement Announcement
	err := json.Unmarshal(data, &announcement)
	return announcement, err
}

func (r *Registry) Upsert(announcement Announcement, addr string, seenAt time.Time) PeerRecord {
	r.mu.Lock()
	defer r.mu.Unlock()

	record := r.peers[announcement.DeviceID]
	record.DeviceID = announcement.DeviceID
	record.DeviceName = announcement.DeviceName
	record.AgentTCPPort = announcement.AgentTCPPort
	record.LastKnownAddr = resolvePeerAddr(announcement, addr)
	record.Online = true
	if !record.ReachabilitySuppressed {
		record.Reachable = record.LastKnownAddr != ""
	}
	record.DiscoverySource = "broadcast"
	record.LastSeenAt = seenAt.UTC()
	r.peers[announcement.DeviceID] = record
	return record
}

func (r *Registry) MarkReachable(deviceID string, reachable bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, ok := r.peers[deviceID]
	if !ok {
		return
	}

	record.Reachable = reachable
	if !reachable {
		record.ReachabilitySuppressed = true
		record.LastDirectActiveAt = time.Time{}
	} else {
		record.ReachabilitySuppressed = false
		record.Reachable = record.LastKnownAddr != ""
	}
	r.peers[deviceID] = record
}

func (r *Registry) MarkDirectActive(deviceID string, addr string, agentTCPPort int, seenAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record := r.peers[deviceID]
	record.DeviceID = deviceID
	if strings.TrimSpace(addr) != "" {
		record.LastKnownAddr = strings.TrimSpace(addr)
	}
	if agentTCPPort > 0 {
		record.AgentTCPPort = agentTCPPort
	}
	record.LastDirectActiveAt = seenAt.UTC()
	record.ReachabilitySuppressed = false
	if record.DiscoverySource == "" {
		record.DiscoverySource = "direct"
	}
	record.Reachable = record.LastKnownAddr != ""
	r.peers[deviceID] = record
}

func (r *Registry) Get(deviceID string) (PeerRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	record, ok := r.peers[deviceID]
	return record, ok
}

func (r *Registry) Snapshot(deviceID string, now time.Time) (PeerRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	record, ok := r.peers[deviceID]
	if !ok {
		return PeerRecord{}, false
	}
	return projectPeerRecord(record, now.UTC()), true
}

func (r *Registry) List() []PeerRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	peers := make([]PeerRecord, 0, len(r.peers))
	now := time.Now().UTC()
	for _, peer := range r.peers {
		peers = append(peers, projectPeerRecord(peer, now))
	}

	sort.Slice(peers, func(i int, j int) bool {
		if peers[i].DeviceName == peers[j].DeviceName {
			return peers[i].DeviceID < peers[j].DeviceID
		}
		return peers[i].DeviceName < peers[j].DeviceName
	})

	return peers
}

func projectPeerRecord(peer PeerRecord, now time.Time) PeerRecord {
	peer.Online = !peer.LastSeenAt.IsZero() && now.Sub(peer.LastSeenAt) <= peerTTL
	broadcastReachable := peer.Online && peer.LastKnownAddr != ""
	directReachable := peer.LastKnownAddr != "" &&
		!peer.LastDirectActiveAt.IsZero() &&
		now.Sub(peer.LastDirectActiveAt) <= directReachabilityTTL
	if peer.ReachabilitySuppressed {
		peer.Reachable = directReachable
	} else {
		peer.Reachable = broadcastReachable || directReachable
	}
	return peer
}

func (r *Runner) Start(ctx context.Context) error {
	listenConn, err := net.ListenPacket("udp4", r.options.ListenAddr)
	if err != nil {
		return err
	}
	r.listenConn = listenConn

	go r.readLoop()

	if r.options.BroadcastAddr != "" {
		go r.broadcastLoop(ctx)
	}

	go func() {
		<-ctx.Done()
		_ = r.Close()
	}()

	return nil
}

func (r *Runner) ListenAddr() string {
	if r.listenConn == nil {
		return r.options.ListenAddr
	}
	return r.listenConn.LocalAddr().String()
}

func (r *Runner) BroadcastOnce() error {
	if r.options.BroadcastAddr == "" {
		return errors.New("broadcast addr is empty")
	}

	if r.broadcastConn == nil {
		conn, err := net.ListenPacket("udp4", ":0")
		if err != nil {
			return err
		}
		r.broadcastConn = conn
	}

	payload, err := Encode(r.options.LocalAnnouncement)
	if err != nil {
		return err
	}

	target, err := net.ResolveUDPAddr("udp4", r.options.BroadcastAddr)
	if err != nil {
		return err
	}

	_, err = r.broadcastConn.WriteTo(payload, target)
	return err
}

func (r *Runner) Close() error {
	var closeErr error
	r.once.Do(func() {
		if r.listenConn != nil {
			closeErr = r.listenConn.Close()
		}
		if r.broadcastConn != nil {
			if err := r.broadcastConn.Close(); closeErr == nil {
				closeErr = err
			}
		}
	})
	return closeErr
}

func (r *Runner) broadcastLoop(ctx context.Context) {
	ticker := time.NewTicker(r.options.Interval)
	defer ticker.Stop()

	_ = r.BroadcastOnce()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.BroadcastOnce()
		}
	}
}

func (r *Runner) readLoop() {
	buffer := make([]byte, 2048)
	for {
		size, addr, err := r.listenConn.ReadFrom(buffer)
		if err != nil {
			return
		}

		announcement, err := Decode(buffer[:size])
		if err != nil {
			continue
		}
		if announcement.DeviceID == r.options.LocalAnnouncement.DeviceID {
			continue
		}
		if r.options.OnAnnouncement != nil {
			r.options.OnAnnouncement(announcement, addr.String(), time.Now().UTC())
		}
	}
}

func resolvePeerAddr(announcement Announcement, addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return net.JoinHostPort(host, strconv.Itoa(announcement.AgentTCPPort))
}
