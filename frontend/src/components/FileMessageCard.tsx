import { useState } from "react";
import type { ConversationMessage, TransferSnapshot } from "../lib/types";
import { ImageViewer } from "./ImageViewer";

const imageExtensions = new Set([".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"]);

function isImageFile(name: string): boolean {
  const lower = name.toLowerCase();
  for (const ext of imageExtensions) {
    if (lower.endsWith(ext)) {
      return true;
    }
  }
  return false;
}

type FileMessageCardProps = {
  message: ConversationMessage;
  transfer?: TransferSnapshot;
  onDelete?: (messageId: string) => void;
};

export function FileMessageCard({ message, transfer, onDelete }: FileMessageCardProps) {
  const [viewerOpen, setViewerOpen] = useState(false);
  const hasTransfer = Boolean(transfer);
  const directionLabel = formatDirectionLabel(transfer?.direction ?? message.direction);
  const transferState = transfer?.state ?? inferMessageTransferState(message);
  const hasTelemetry = hasTransferTelemetry(transfer, transferState);
  const isImage = isImageFile(transfer?.fileName ?? message.body);
  const fileRemoved = message.kind === "file" && message.fileExists === false;
  const rawPercent = hasTelemetry
    ? clampPercent(
        transfer?.progressPercent ??
          calculateFallbackProgress(transfer?.bytesTransferred, transfer?.fileSize),
      )
    : 0;
  const displayPercent = hasTelemetry ? formatDisplayPercent(rawPercent, transferState) : 0;
  const progressLabel = `${transfer?.fileName ?? message.body} 传输进度`;
  const ratio = hasTelemetry
    ? `${formatFileSize(transfer?.bytesTransferred ?? 0)} / ${formatFileSize(transfer?.fileSize ?? 0)}`
    : null;
  const rateLabel =
    hasTransfer && (transfer?.rateBytesPerSec ?? 0) > 0
      ? `速率 ${formatRate(transfer?.rateBytesPerSec ?? 0)}`
      : null;
  const etaLabel = formatEta(transfer?.etaSeconds, transferState);
  const stateLabel = formatTransferState(transferState);
  const progressValueText = hasTelemetry
    ? formatProgressValueText(displayPercent, ratio, rateLabel, etaLabel, stateLabel)
    : stateLabel;

  return (
    <article className={`ms-file-card${fileRemoved ? " ms-file-card--missing" : ""}`}>
      <div className="ms-file-card__header">
        <span className="ms-eyebrow">{directionLabel}</span>
        <div className="ms-file-card__header-actions">
          <span className="ms-file-card__time">{formatMessageTime(message.createdAt)}</span>
          {onDelete ? (
            <button
              aria-label="删除消息"
              className="ms-icon-button ms-message-delete-button"
              onClick={() => onDelete(message.messageId)}
              title="删除消息"
              type="button"
            >
              <svg
                aria-hidden="true"
                fill="none"
                focusable="false"
                strokeLinecap="round"
                strokeLinejoin="round"
                viewBox="0 0 16 16"
                width="14"
                height="14"
              >
                <path d="M2 4h12" stroke="currentColor" strokeWidth="1.4" />
                <path
                  d="M5.333 4V2.667a1.333 1.333 0 0 1 1.334-1.334h2.666a1.333 1.333 0 0 1 1.334 1.334V4"
                  stroke="currentColor"
                  strokeWidth="1.4"
                />
                <path
                  d="M3.333 4v9.333a1.333 1.333 0 0 0 1.334 1.334h6.666a1.333 1.333 0 0 0 1.334-1.334V4"
                  stroke="currentColor"
                  strokeWidth="1.4"
                />
              </svg>
            </button>
          ) : null}
          {fileRemoved ? (
            <button
              aria-label="清除已移除文件的消息"
              className="ms-icon-button ms-message-clear-button"
              onClick={() => onDelete?.(message.messageId)}
              title="文件已移除，点击清除"
              type="button"
            >
              <svg
                aria-hidden="true"
                fill="none"
                focusable="false"
                strokeLinecap="round"
                strokeLinejoin="round"
                viewBox="0 0 16 16"
                width="14"
                height="14"
              >
                <line x1="4" y1="4" x2="12" y2="12" stroke="currentColor" strokeWidth="1.4" />
                <line x1="12" y1="4" x2="4" y2="12" stroke="currentColor" strokeWidth="1.4" />
              </svg>
            </button>
          ) : null}
        </div>
      </div>

      {isImage && message.filePath && !fileRemoved ? (
        <div className="ms-file-card__thumbnail">
          <img
            className="ms-file-card__thumbnail-img"
            src={`file://${message.filePath}`}
            alt={transfer?.fileName ?? message.body}
            onDoubleClick={() => setViewerOpen(true)}
            title="双击查看原图"
          />
        </div>
      ) : null}

      <div className="ms-file-card__title">{transfer?.fileName ?? message.body}</div>

      {fileRemoved ? (
        <div className="ms-file-card__missing">
          <span className="ms-file-card__missing-label">文件已移除</span>
        </div>
      ) : null}

      {hasTransfer && hasTelemetry && !fileRemoved ? (
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
        {hasTransfer && hasTelemetry && !fileRemoved && ratio ? <span>{ratio}</span> : null}
        {rateLabel ? <span>{rateLabel}</span> : null}
        {etaLabel ? <span>{etaLabel}</span> : null}
        <span>{stateLabel}</span>
      </div>

      {viewerOpen && message.filePath ? (
        <ImageViewer
          src={`file://${message.filePath}`}
          alt={transfer?.fileName ?? message.body}
          onClose={() => setViewerOpen(false)}
        />
      ) : null}
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
  if (fileSize >= 1024 * 1024 * 1024) {
    return `${Math.max(1, Math.round((fileSize / (1024 * 1024 * 1024)) * 10) / 10)} GB`;
  }
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
  if (
    state === "done" ||
    state === "received" ||
    state === "sent" ||
    state === "failed" ||
    state === "fallback_pending" ||
    state === "fallback_transferring"
  ) {
    return true;
  }
  return transfer.bytesTransferred > 0 || transfer.progressPercent > 0 || transfer.rateBytesPerSec > 0;
}
