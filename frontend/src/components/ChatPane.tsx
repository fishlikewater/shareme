import { useLayoutEffect, useRef, useState, type ChangeEvent, type FormEvent } from "react";

import { FileMessageCard } from "./FileMessageCard";
import type { ConversationMessage, LocalFileSnapshot, PeerSummary } from "../lib/types";

type ChatPaneProps = {
  peer?: PeerSummary;
  conversationId?: string;
  messages: ConversationMessage[];
  sendingText: boolean;
  sendingFile: boolean;
  pickingLocalFile: boolean;
  sendingAcceleratedFile: boolean;
  pickedLocalFile: LocalFileSnapshot | null;
  historyHasMore: boolean;
  historyLoading: boolean;
  historyError?: string;
  onSendText: (body: string) => Promise<void>;
  onSendFile: (file: File) => Promise<void>;
  onPickLocalFile: () => Promise<void>;
  onSendAcceleratedFile: () => Promise<void>;
  onLoadOlderMessages: () => Promise<void>;
};

export function ChatPane({
  peer,
  conversationId,
  messages,
  sendingText,
  sendingFile,
  pickingLocalFile,
  sendingAcceleratedFile,
  pickedLocalFile,
  historyHasMore,
  historyLoading,
  historyError,
  onSendText,
  onSendFile,
  onPickLocalFile,
  onSendAcceleratedFile,
  onLoadOlderMessages,
}: ChatPaneProps) {
  const [draft, setDraft] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);
  const messageListRef = useRef<HTMLDivElement>(null);
  const prependAnchorHeightRef = useRef<number | null>(null);
  const previousConversationIDRef = useRef<string | undefined>(undefined);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const body = draft.trim();
    if (!body) {
      return;
    }

    await onSendText(body);
    setDraft("");
  }

  async function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }

    await onSendFile(file);
    event.target.value = "";
  }

  function handlePickFile() {
    if (sendingFile || pickingLocalFile || sendingAcceleratedFile) {
      return;
    }
    fileInputRef.current?.click();
  }

  async function handlePickLocalFile() {
    if (sendingFile || pickingLocalFile || sendingAcceleratedFile) {
      return;
    }
    await onPickLocalFile();
  }

  async function handleSendPickedLocalFile() {
    if (!pickedLocalFile || sendingAcceleratedFile) {
      return;
    }
    await onSendAcceleratedFile();
  }

  async function handleMessageListScroll() {
    const container = messageListRef.current;
    if (!container || !historyHasMore || historyLoading) {
      return;
    }
    if (container.scrollTop > 48) {
      return;
    }
    prependAnchorHeightRef.current = container.scrollHeight;
    await onLoadOlderMessages();
  }

  useLayoutEffect(() => {
    const container = messageListRef.current;
    if (!container || prependAnchorHeightRef.current === null) {
      return;
    }
    const delta = container.scrollHeight - prependAnchorHeightRef.current;
    container.scrollTop += Math.max(delta, 0);
    prependAnchorHeightRef.current = null;
  }, [messages.length]);

  useLayoutEffect(() => {
    const container = messageListRef.current;
    if (!container || !conversationId) {
      previousConversationIDRef.current = conversationId;
      return;
    }
    if (previousConversationIDRef.current !== conversationId) {
      previousConversationIDRef.current = conversationId;
      container.scrollTop = container.scrollHeight;
    }
  }, [conversationId]);

  const canSend = Boolean(peer?.trusted && peer.reachable);
  const acceleratedBusy = pickingLocalFile || sendingAcceleratedFile;

  return (
    <section className="ms-panel ms-chat-panel">
      <div className="ms-chat-header">
        <div>
          <span className="ms-eyebrow">沟通中心</span>
          <h2 className="ms-chat-title">
            {peer ? `连接 ${peer.deviceName}` : "请选择一台设备开始传输"}
          </h2>
          <p className="ms-chat-copy">{resolveChatCopy(peer)}</p>
        </div>
        {peer ? (
          <div className="ms-badge-row">
            <span className={`ms-badge ${peer.trusted ? "ms-badge--ok" : "ms-badge--warn"}`}>
              {peer.trusted ? "已信任" : "未信任"}
            </span>
            <span className={`ms-badge ${peer.reachable ? "ms-badge--neutral" : "ms-badge--ghost"}`}>
              {peer.reachable ? "可连接" : "连接受限"}
            </span>
          </div>
        ) : null}
      </div>

      {!peer ? (
        <section className="ms-workspace-empty">
          <div className="ms-workspace-empty__hero">
            <span className="ms-chip ms-chip--soft">等待设备</span>
            <strong>先选一台设备，再开始点对点传输</strong>
            <p>在左侧列表中选择设备，完成配对后即可互传文字与文件。</p>
          </div>
          <div className="ms-workspace-steps" aria-label="快速引导">
            <div className="ms-workspace-step">
              <span className="ms-workspace-step__index">1</span>
              <div>
                <strong>选择设备</strong>
                <p>点击目标设备卡片，确认它是否在线并可直连。</p>
              </div>
            </div>
            <div className="ms-workspace-step">
              <span className="ms-workspace-step__index">2</span>
              <div>
                <strong>确认配对</strong>
                <p>对照短码建立信任，后续就能重复使用。</p>
              </div>
            </div>
            <div className="ms-workspace-step">
              <span className="ms-workspace-step__index">3</span>
              <div>
                <strong>开始传输</strong>
                <p>文字、小文件和大文件都能直接发送。</p>
              </div>
            </div>
          </div>
        </section>
      ) : null}
      {peer && !peer.trusted ? (
        <div className="ms-chat-blocker">尚未完成信任，暂不可发送内容。</div>
      ) : null}
      {peer && peer.trusted && !peer.reachable ? (
        <div className="ms-chat-blocker">当前设备暂时不可达，请稍后重试。</div>
      ) : null}

      {canSend ? (
        <>
          <div className="ms-section-head">
            <span className="ms-section-title">实时沟通</span>
            <span className="ms-section-hint">可以开始发送文字、图片以外的任意文件</span>
          </div>

          <section className="ms-accelerated-card">
            <div className="ms-accelerated-card__header">
              <div>
                <span className="ms-eyebrow">极速路径</span>
                <h3 className="ms-accelerated-card__title">大文件直读本地磁盘</h3>
                <p className="ms-accelerated-card__copy">
                  由本机 agent 直接读取磁盘文件，减少浏览器中转和重复落盘。
                </p>
              </div>
              <button
                className="ms-button ms-button--secondary"
                disabled={acceleratedBusy}
                onClick={() => {
                  void handlePickLocalFile();
                }}
                type="button"
              >
                {pickingLocalFile ? "选择中..." : "极速发送大文件"}
              </button>
            </div>

            {pickedLocalFile ? (
              <div className="ms-local-file-card">
                <div className="ms-local-file-card__header">
                  <div>
                    <span className="ms-local-file-card__label">已选本地文件</span>
                    <strong className="ms-local-file-card__name">{pickedLocalFile.displayName}</strong>
                  </div>
                  <span
                    className={`ms-badge ${
                      pickedLocalFile.acceleratedEligible ? "ms-badge--ok" : "ms-badge--warn"
                    }`}
                  >
                    {pickedLocalFile.acceleratedEligible ? "满足极速条件" : "不满足极速条件"}
                  </span>
                </div>
                <div className="ms-local-file-card__meta">
                  <span>{formatFileSize(pickedLocalFile.size)}</span>
                  <span>
                    {pickedLocalFile.acceleratedEligible
                      ? "将优先走高速数据面"
                      : "当前文件会继续走普通文件传输"}
                  </span>
                </div>
                <div className="ms-local-file-card__actions">
                  <button
                    className="ms-button ms-button--primary"
                    disabled={sendingAcceleratedFile}
                    onClick={() => {
                      void handleSendPickedLocalFile();
                    }}
                    type="button"
                  >
                    {sendingAcceleratedFile
                      ? "发送中..."
                      : pickedLocalFile.acceleratedEligible
                        ? "发送已选大文件"
                        : "发送已选文件"}
                  </button>
                </div>
              </div>
            ) : (
              <div className="ms-accelerated-card__empty">
                选择一个本地大文件后，这里会展示极速发送资格与发送入口。
              </div>
            )}
          </section>

          <div
            ref={messageListRef}
            aria-label="消息列表"
            className="ms-message-list"
            data-testid="message-list"
            onScroll={() => {
              void handleMessageListScroll();
            }}
          >
            <div className="ms-message-list__status" role="status">
              {historyLoading
                ? "正在加载更早消息..."
                : historyError
                  ? historyError
                  : historyHasMore
                    ? "继续上滑可加载更早消息"
                    : messages.length > 0
                      ? "已显示全部历史消息"
                      : ""}
            </div>
            {messages.length === 0 ? (
              <div className="ms-empty-card">还没有消息，先发一条试试看。</div>
            ) : null}

            {messages.map((message) => {
              if (message.kind === "file") {
                return (
                  <FileMessageCard
                    key={message.messageId}
                    message={message}
                    transfer={message.transfer}
                  />
                );
              }

              return (
                <article
                  key={message.messageId}
                  className={`ms-message-card ${
                    message.direction === "outgoing" ? "ms-message-card--outgoing" : "ms-message-card--incoming"
                  }`}
                >
                  <div className="ms-message-card__top">
                    <strong className="ms-message-kind">信息</strong>
                    <span className="ms-message-time">{formatMessageTime(message.createdAt)}</span>
                  </div>
                  <div className="ms-message-body">{message.body}</div>
                  <div className="ms-message-meta">{formatMessageState(message.status)}</div>
                </article>
              );
            })}
          </div>

          <form className="ms-composer" onSubmit={handleSubmit}>
            <textarea
              aria-label="消息输入框"
              className="ms-textarea"
              disabled={sendingText}
              onChange={(event) => setDraft(event.target.value)}
              placeholder="输入一条消息，或直接发送文件"
              rows={4}
              value={draft}
            />
            <div className="ms-button-row">
              <button className="ms-button ms-button--primary" disabled={sendingText || draft.trim() === ""} type="submit">
                {sendingText ? "发送中..." : "发送文字"}
              </button>
              <button
                className="ms-button ms-button--secondary"
                disabled={sendingFile || pickingLocalFile || sendingAcceleratedFile}
                onClick={handlePickFile}
                type="button"
              >
                {sendingFile ? "文件上传中..." : "选择文件"}
              </button>
              <input
                aria-hidden="true"
                ref={fileInputRef}
                className="ms-visually-hidden"
                data-testid="file-input"
                disabled={sendingFile || pickingLocalFile || sendingAcceleratedFile}
                onChange={handleFileChange}
                tabIndex={-1}
                type="file"
              />
            </div>
          </form>
        </>
      ) : null}
    </section>
  );
}

function resolveChatCopy(peer?: PeerSummary): string {
  if (!peer) {
    return "请选择一台设备，在列表中查看状态并建立连接。";
  }
  if (!peer.trusted) {
    return "完成配对后即可安全通讯。";
  }
  if (!peer.reachable) {
    return "设备已信任，但当前不可达，请稍后重试。";
  }
  return "可以开始发送文字、图片以外的任意文件";
}

function formatMessageState(status: string): string {
  if (status === "sent") {
    return "已发送";
  }
  if (status === "sending") {
    return "发送中";
  }
  return status;
}

function formatMessageTime(value: string): string {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return value;
  }

  return new Intl.DateTimeFormat("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    month: "numeric",
    day: "numeric",
  }).format(new Date(timestamp));
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
