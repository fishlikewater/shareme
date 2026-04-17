import type { TransferSnapshot } from "../lib/types";

type TransferStatusBannerProps = {
  transfers: TransferSnapshot[];
};

export function TransferStatusBanner({ transfers }: TransferStatusBannerProps) {
  const activeTransfers = transfers.filter((transfer) => Boolean(transfer.active));
  if (activeTransfers.length === 0) {
    return null;
  }

  return (
    <section className="ms-transfer-banner" aria-label="传输横幅">
      <div className="ms-transfer-banner__header">
        <div>
          <span className="ms-eyebrow">文件传输</span>
          <p className="ms-transfer-banner__lead">正在保持传输进度，随时掌握状态。</p>
        </div>
      </div>
      <div className="ms-transfer-banner__grid">
        {activeTransfers.map((transfer) => {
          const hasTelemetry = hasTransferTelemetry(transfer);
          const rawPercent = hasTelemetry ? clampPercent(transfer.progressPercent) : 0;
          const displayPercent = hasTelemetry ? formatDisplayPercent(rawPercent, transfer.state) : 0;
          const progressLabel = `${transfer.fileName} 传输进度`;
          const ratio = hasTelemetry
            ? `${formatFileSize(transfer.bytesTransferred)} / ${formatFileSize(transfer.fileSize)}`
            : null;
          const rateLabel =
            transfer.rateBytesPerSec > 0 ? `速率 ${formatRate(transfer.rateBytesPerSec)}` : null;
          const etaLabel = formatEta(transfer.etaSeconds, transfer.state);
          const stateLabel = formatTransferState(transfer.state);
          const progressValueText = hasTelemetry
            ? formatProgressValueText(displayPercent, ratio, rateLabel, etaLabel, stateLabel)
            : stateLabel;
          return (
            <article key={transfer.transferId} className="ms-transfer-banner__card">
              <div className="ms-transfer-banner__top">
                <strong className="ms-transfer-banner__title">{transfer.fileName}</strong>
                <span className="ms-transfer-banner__direction">
                  {formatDirectionLabel(transfer.direction)}
                </span>
              </div>
              {hasTelemetry ? (
                <div className="ms-transfer-banner__progress">
                  <div
                    className="ms-transfer-banner__progress-track"
                    role="progressbar"
                    aria-label={progressLabel}
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-valuenow={displayPercent}
                    aria-valuetext={progressValueText}
                  >
                    <span
                      className="ms-transfer-banner__progress-fill"
                      style={{ width: `${rawPercent}%` }}
                    />
                  </div>
                  <span className="ms-transfer-banner__percent">{displayPercent}%</span>
                </div>
              ) : null}
              <div className="ms-transfer-banner__details">
                {hasTelemetry && ratio ? <span>{ratio}</span> : null}
                {rateLabel ? <span>{rateLabel}</span> : null}
                {etaLabel ? <span>{etaLabel}</span> : null}
                <span>{stateLabel}</span>
              </div>
            </article>
          );
        })}
      </div>
    </section>
  );
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.max(0, Math.min(100, value));
}

function formatDisplayPercent(value: number, state: string): number {
  if (state === "done") {
    return 100;
  }
  return Math.min(99, Math.round(value));
}

function formatProgressValueText(
  percent: number,
  ratio: string | null,
  rateLabel: string | null,
  etaLabel: string | null,
  stateLabel: string,
): string {
  const parts = [`已传输 ${percent}%`];
  if (ratio) {
    parts.push(ratio);
  }
  if (rateLabel) {
    parts.push(rateLabel);
  }
  if (etaLabel) {
    parts.push(etaLabel);
  }
  parts.push(stateLabel);
  return parts.join("，");
}

function formatFileSize(fileSize: number): string {
  if (fileSize >= 1024 * 1024) {
    return `${Math.max(1, Math.round(fileSize / (1024 * 1024)))} MB`;
  }
  if (fileSize >= 1024) {
    return `${Math.max(1, Math.round(fileSize / 1024))} KB`;
  }
  return `${fileSize} B`;
}

function formatRate(bytesPerSec: number): string {
  if (bytesPerSec >= 1024 * 1024) {
    return `${Math.max(1, Math.round(bytesPerSec / (1024 * 1024)))} MB/s`;
  }
  if (bytesPerSec >= 1024) {
    return `${Math.max(1, Math.round(bytesPerSec / 1024))} KB/s`;
  }
  return `${bytesPerSec} B/s`;
}

function formatEta(seconds: number | null | undefined, state: string): string | null {
  if (
    state === "done" ||
    state === "failed" ||
    state === "received" ||
    state === "sent" ||
    state === "fallback_pending" ||
    state === "fallback_transferring"
  ) {
    return null;
  }
  if (!Number.isFinite(seconds) || seconds == null || seconds <= 0) {
    return null;
  }
  const roundedSeconds = Math.max(1, Math.round(seconds));
  const minutes = Math.floor(roundedSeconds / 60);
  const remainder = roundedSeconds % 60;
  if (minutes === 0) {
    return `预计 ${roundedSeconds} 秒`;
  }
  if (remainder === 0) {
    return `预计 ${minutes} 分钟`;
  }
  return `预计 ${minutes} 分 ${remainder} 秒`;
}

function formatTransferState(state: string): string {
  if (state === "preparing") {
    return "准备极速传输";
  }
  if (state === "fallback_pending") {
    return "准备回退普通传输";
  }
  if (state === "fallback_transferring") {
    return "已回退普通传输";
  }
  if (state === "receiving") {
    return "接收中";
  }
  if (state === "received") {
    return "已接收";
  }
  if (state === "done") {
    return "已完成";
  }
  if (state === "sending") {
    return "传输中";
  }
  if (state === "sent") {
    return "已发送";
  }
  if (state === "failed") {
    return "传输失败";
  }
  return state;
}

function formatDirectionLabel(direction?: string): string {
  if (direction === "incoming") {
    return "接收";
  }
  if (direction === "outgoing") {
    return "发送";
  }
  return "文件";
}

function hasTransferTelemetry(transfer: TransferSnapshot): boolean {
  if (
    transfer.state === "done" ||
    transfer.state === "received" ||
    transfer.state === "sent" ||
    transfer.state === "failed" ||
    transfer.state === "fallback_pending" ||
    transfer.state === "fallback_transferring"
  ) {
    return true;
  }
  return transfer.bytesTransferred > 0 || transfer.progressPercent > 0 || transfer.rateBytesPerSec > 0;
}
