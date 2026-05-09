import type { PeerSummary } from "../lib/types";

type PairCodeDialogProps = {
  peer?: PeerSummary;
  busy: boolean;
  onStartPairing: () => void;
  onConfirmPairing: (pairingId: string) => void;
};

export function PairCodeDialog({
  peer,
  busy,
  onStartPairing,
  onConfirmPairing,
}: PairCodeDialogProps) {
  if (!peer || peer.trusted) {
    return null;
  }

  const pairingStatus = peer.pairing?.status;
  const confirmedButPendingTrust = pairingStatus === "confirmed";

  return (
    <section className="ms-panel ms-trust-task ms-pairing-card" aria-label="信任建立">
      <div className="ms-pairing-card__copy">
        <span className="ms-eyebrow">信任建立</span>
        <h3 className="ms-pairing-card__title">建立信任后再发送</h3>
        <p className="ms-pairing-card__body">{resolveBody(peer)}</p>
      </div>

      <ol className="ms-pairing-steps">
        <li>先发起配对</li>
        <li>核对两端是否看到相同短码</li>
        <li>确认后立刻开启点对点直传</li>
      </ol>

      {peer.pairing?.shortCode ? (
        <div className="ms-pairing-code">
          <span className="ms-pairing-code__label">本次短码</span>
          <strong>{peer.pairing.shortCode}</strong>
        </div>
      ) : null}

      <div className="ms-button-row">
        {peer.reachable && !peer.pairing ? (
          <button className="ms-button ms-button--primary" disabled={busy} onClick={onStartPairing} type="button">
            开始配对
          </button>
        ) : null}
        {peer.pairing?.pairingId && !confirmedButPendingTrust ? (
          <button
            className="ms-button ms-button--secondary"
            disabled={busy}
            onClick={() => onConfirmPairing(peer.pairing!.pairingId)}
            type="button"
          >
            确认配对
          </button>
        ) : null}
        {confirmedButPendingTrust ? (
          <span className="ms-chip ms-chip--soft" role="status">
            已确认，等待同步
          </span>
        ) : null}
      </div>
    </section>
  );
}

function resolveBody(peer: PeerSummary): string {
  if (!peer.reachable) {
    return "设备当前不可达，等它重新上线后才能继续配对。";
  }
  if (peer.pairing?.status === "confirmed") {
    return "已确认短码，正在等待设备完成信任同步。";
  }
  if (peer.pairing) {
    return "短码已经生成，请对照两端显示的六码短码后完成确认。";
  }
  return "这台设备还没建立信任，配对一次后，后续文字和文件都能直接发送。";
}
