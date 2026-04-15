import type { ConversationMessage, TransferSnapshot } from "../lib/types";

type FileMessageCardProps = {
  message: ConversationMessage;
  transfer?: TransferSnapshot;
};

export function FileMessageCard({ message, transfer }: FileMessageCardProps) {
  const hasTransfer = Boolean(transfer);
  const directionLabel = formatDirectionLabel(transfer?.direction ?? message.direction);
  const transferState = transfer?.state ?? inferMessageTransferState(message);
  const hasTelemetry = hasTransferTelemetry(transfer, transferState);
  const rawPercent = hasTelemetry
    ? clampPercent(
        transfer?.progressPercent ??
          calculateFallbackProgress(transfer?.bytesTransferred, transfer?.fileSize),
      )
    : 0;
  const displayPercent = hasTelemetry ? formatDisplayPercent(rawPercent, transferState) : 0;
  const progressLabel = `${transfer?.fileName ?? message.body} 传输进度`;
  const ratio = hasTelemetry
    ? `${formatFileSize(transfer?.bytesTransferred ?? 0)} / ${formatFileSize(
        transfer?.fileSize ?? 0,
      )}`
    : null;
  const rateLabel = hasTelemetry ? `速率 ${formatRate(transfer?.rateBytesPerSec ?? 0)}` : null;
  const etaLabel = formatEta(transfer?.etaSeconds, transferState);
  const stateLabel = formatTransferState(transferState);
  const progressValueText =
    hasTelemetry && ratio && rateLabel
      ? formatProgressValueText(displayPercent, ratio, rateLabel, etaLabel, stateLabel)
      : stateLabel;

  return (
    <article className="ms-file-card">
      <div className="ms-file-card__header">
        <span className="ms-eyebrow">{directionLabel}</span>
        <span className="ms-file-card__time">{formatMessageTime(message.createdAt)}</span>
      </div>
      <div className="ms-file-card__title">{transfer?.fileName ?? message.body}</div>
      {hasTransfer && hasTelemetry ? (
        <div className="ms-file-card__progress">
          <div
            className="ms-file-card__progress-track"
            role="progressbar"
            aria-label={progressLabel}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={displayPercent}
            aria-valuetext={progressValueText}
          >
            <span className="ms-file-card__progress-bar" style={{ width: `${rawPercent}%` }} />
          </div>
          <span className="ms-file-card__percent">{displayPercent}%</span>
        </div>
      ) : null}
      <div className="ms-file-card__meta">
        {hasTransfer && hasTelemetry && ratio && rateLabel ? (
          <>
            <span>{ratio}</span>
            <span>{rateLabel}</span>
            {etaLabel ? <span>{etaLabel}</span> : null}
          </>
        ) : null}
        <span>{stateLabel}</span>
      </div>
    </article>
  );
}

function calculateFallbackProgress(bytes?: number, total?: number): number {
  if (!bytes || !total) {
    return 0;
  }
  return (bytes / total) * 100;
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
  ratio: string,
  rateLabel: string,
  etaLabel: string | null,
  stateLabel: string,
): string {
  const parts = [`已传输 ${percent}%`, ratio, rateLabel];
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
  if (state === "done" || state === "failed") {
    return null;
  }
  if (!Number.isFinite(seconds) || seconds == null || seconds <= 0) {
    return null;
  }
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  const paddedSeconds = String(remainder).padStart(2, "0");
  const paddedMinutes = String(minutes).padStart(2, "0");
  return `ETA ${paddedMinutes}:${paddedSeconds}`;
}

function formatTransferState(state: string): string {
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

function formatMessageTime(value: string): string {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return value;
  }
  return new Intl.DateTimeFormat("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(timestamp));
}

function inferMessageTransferState(message: ConversationMessage): string {
  if (message.kind !== "file") {
    return message.status;
  }
  if (message.direction === "incoming" && message.status === "sent") {
    return "received";
  }
  return message.status;
}

function hasTransferTelemetry(transfer: TransferSnapshot | undefined, state: string): boolean {
  if (!transfer) {
    return false;
  }
  if (state === "done" || state === "received" || state === "sent" || state === "failed") {
    return true;
  }
  return transfer.bytesTransferred > 0 || transfer.progressPercent > 0 || transfer.rateBytesPerSec > 0;
}
