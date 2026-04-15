import type { PeerSummary } from "../lib/types";

type DeviceListProps = {
  peers: PeerSummary[];
  selectedPeerId?: string;
  onSelect: (peer: PeerSummary) => void;
};

export function DeviceList({ peers, selectedPeerId, onSelect }: DeviceListProps) {
  const trustedCount = peers.filter((peer) => peer.trusted).length;
  const readyCount = peers.filter((peer) => peer.trusted && peer.reachable).length;
  const pendingCount = peers.filter((peer) => !peer.trusted).length;

  return (
    <section className="ms-panel ms-panel--dark ms-device-panel">
      <div className="ms-panel-heading">
        <span className="ms-eyebrow ms-eyebrow--bright">设备面板</span>
        <div className="ms-device-panel__title-row">
          <h2 className="ms-panel-title ms-panel-title--light">已发现 {peers.length} 台设备</h2>
          <span className="ms-chip ms-chip--outline">{trustedCount} 台已配对</span>
        </div>
        <p className="ms-panel-copy ms-panel-copy--light">
          选中一台设备，马上开始发送文字或文件。
        </p>
        <div className="ms-inline-meta">
          <span>可立即发送 {readyCount}</span>
          <span>待配对 {pendingCount}</span>
        </div>
      </div>

      <div aria-label="设备列表" className="ms-device-list">
        {peers.length === 0 ? (
          <div className="ms-empty-card">正在等待局域网里的设备出现</div>
        ) : (
          peers.map((peer) => (
            <button
              aria-pressed={peer.deviceId === selectedPeerId}
              key={peer.deviceId}
              className={`ms-device-card${peer.deviceId === selectedPeerId ? " is-active" : ""}`}
              onClick={() => onSelect(peer)}
              type="button"
            >
              <div className="ms-device-card__header">
                <div>
                  <span className="ms-device-card__name">{peer.deviceName}</span>
                  <span className="ms-device-card__meta">{describePeer(peer)}</span>
                </div>
                <span className={`ms-status-dot ${resolveStatusClass(peer)}`} aria-hidden="true" />
              </div>

              <div className="ms-badge-row">
                <span className={`ms-badge ${peer.trusted ? "ms-badge--ok" : "ms-badge--warn"}`}>
                  {peer.trusted ? "已配对" : "待配对"}
                </span>
                <span className={`ms-badge ${peer.reachable ? "ms-badge--neutral" : "ms-badge--ghost"}`}>
                  {peer.reachable ? "可达" : "不可达"}
                </span>
              </div>

              <p className="ms-device-card__preview">{peer.lastMessagePreview ?? resolvePreview(peer)}</p>
            </button>
          ))
        )}
      </div>
    </section>
  );
}

function describePeer(peer: PeerSummary): string {
  if (!peer.reachable) {
    return "已发现，但暂时无法直连";
  }
  if (!peer.trusted) {
    return peer.pairing ? "等待你确认短码" : "尚未建立信任";
  }
  if (!peer.online) {
    return "已配对，可继续直连";
  }
  return "已配对，可立即发送";
}

function resolvePreview(peer: PeerSummary): string {
  if (!peer.trusted) {
    return "先完成配对，再开始发送文字或文件";
  }
  if (!peer.reachable) {
    return "设备已发现，但当前无法建立直连传输";
  }
  return "可以开始发送文字、图片以外的任意文件";
}

function resolveStatusClass(peer: PeerSummary): string {
  if (peer.trusted && peer.reachable) {
    return "ms-status-dot--ok";
  }
  if (!peer.trusted && peer.reachable) {
    return "ms-status-dot--warn";
  }
  return "ms-status-dot--muted";
}
