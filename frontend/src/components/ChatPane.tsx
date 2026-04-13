import { useRef, useState, type ChangeEvent, type FormEvent } from "react";

import type { ConversationMessage, PeerSummary } from "../lib/types";

type ChatPaneProps = {
  peer?: PeerSummary;
  messages: ConversationMessage[];
  sendingText: boolean;
  sendingFile: boolean;
  onSendText: (body: string) => Promise<void>;
  onSendFile: (file: File) => Promise<void>;
};

export function ChatPane({
  peer,
  messages,
  sendingText,
  sendingFile,
  onSendText,
  onSendFile,
}: ChatPaneProps) {
  const [draft, setDraft] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);

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
    if (sendingFile) {
      return;
    }
    fileInputRef.current?.click();
  }

  const canSend = Boolean(peer?.trusted && peer.reachable);

  return (
    <section className="ms-panel ms-chat-panel">
      <div className="ms-chat-header">
        <div>
          <span className="ms-eyebrow">传输工作区</span>
          <h2 className="ms-chat-title">{peer ? `与 ${peer.deviceName} 的对话` : "请选择一台设备"}</h2>
          <p className="ms-chat-copy">{resolveChatCopy(peer)}</p>
        </div>
        {peer ? (
          <div className="ms-badge-row">
            <span className={`ms-badge ${peer.trusted ? "ms-badge--ok" : "ms-badge--warn"}`}>
              {peer.trusted ? "已配对" : "待配对"}
            </span>
            <span className={`ms-badge ${peer.reachable ? "ms-badge--neutral" : "ms-badge--ghost"}`}>
              {peer.reachable ? "可直连" : "当前不可达"}
            </span>
          </div>
        ) : null}
      </div>

      {!peer ? (
        <section className="ms-workspace-empty">
          <div className="ms-workspace-empty__hero">
            <span className="ms-chip ms-chip--soft">首次进入</span>
            <strong>设备出现后，点一下设备卡片，就能开始发送。</strong>
            <p>不需要账号。只要在同一局域网里，首次配对一次，后续就能反复直传文字和文件。</p>
          </div>

          <div className="ms-workspace-steps" aria-label="开始使用步骤">
            <div className="ms-workspace-step">
              <span className="ms-workspace-step__index">1</span>
              <div>
                <strong>等待发现设备</strong>
                <p>设备列表会自动列出同网段内在线设备。</p>
              </div>
            </div>
            <div className="ms-workspace-step">
              <span className="ms-workspace-step__index">2</span>
              <div>
                <strong>首次建立信任</strong>
                <p>看到短码后，两端确认一致即可完成配对。</p>
              </div>
            </div>
            <div className="ms-workspace-step">
              <span className="ms-workspace-step__index">3</span>
              <div>
                <strong>直接发送文字或文件</strong>
                <p>配对完成后，消息区会立即切换为传输工作台。</p>
              </div>
            </div>
          </div>
        </section>
      ) : null}
      {peer && !peer.trusted ? (
        <div className="ms-chat-blocker">完成配对后，这里会开放文字与文件直传。</div>
      ) : null}
      {peer && peer.trusted && !peer.reachable ? (
        <div className="ms-chat-blocker">设备当前不可达，先等待它重新上线。</div>
      ) : null}

      {canSend ? (
        <>
          <div className="ms-section-head">
            <span className="ms-section-title">最近消息</span>
            <span className="ms-section-hint">可以直接发送文字或文件</span>
          </div>

          <div className="ms-message-list">
            {messages.length === 0 ? (
              <div className="ms-empty-card">这台设备还没有消息记录，现在就可以开始第一条传输。</div>
            ) : null}

            {messages.map((message) => (
              <article
                key={message.messageId}
                className={`ms-message-card ${
                  message.direction === "outgoing" ? "ms-message-card--outgoing" : "ms-message-card--incoming"
                }`}
              >
                <div className="ms-message-card__top">
                  <strong className="ms-message-kind">{message.kind === "file" ? "文件" : "文字"}</strong>
                  <span className="ms-message-time">{formatMessageTime(message.createdAt)}</span>
                </div>
                <div className="ms-message-body">{message.body}</div>
                <div className="ms-message-meta">
                  {message.transfer
                    ? `${formatFileSize(message.transfer.fileSize)} · ${formatTransferState(message.transfer.state)}`
                    : formatMessageState(message.status)}
                </div>
              </article>
            ))}
          </div>

          <form className="ms-composer" onSubmit={handleSubmit}>
            <textarea
              aria-label="消息输入框"
              className="ms-textarea"
              disabled={sendingText}
              onChange={(event) => setDraft(event.target.value)}
              placeholder="输入一句话，或者选择一个文件立即发送"
              rows={4}
              value={draft}
            />
            <div className="ms-button-row">
              <button className="ms-button ms-button--primary" disabled={sendingText || draft.trim() === ""} type="submit">
                {sendingText ? "发送中..." : "发送文字"}
              </button>
              <button
                className="ms-button ms-button--secondary"
                disabled={sendingFile}
                onClick={handlePickFile}
                type="button"
              >
                {sendingFile ? "发送文件中..." : "选择文件"}
              </button>
              <input
                aria-hidden="true"
                ref={fileInputRef}
                className="ms-visually-hidden"
                data-testid="file-input"
                disabled={sendingFile}
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
    return "先在设备列表里选中一台设备，再继续配对、发文字或发文件。";
  }
  if (!peer.trusted) {
    return "这台设备还没建立信任，请先完成配对。";
  }
  if (!peer.reachable) {
    return "已经配对，但它当前不在线。";
  }
  return "可以直接发送文字或文件";
}

function formatFileSize(fileSize: number): string {
  if (fileSize >= 1024 * 1024) {
    return `${(fileSize / (1024 * 1024)).toFixed(1)} MB`;
  }
  if (fileSize >= 1024) {
    return `${(fileSize / 1024).toFixed(1)} KB`;
  }
  return `${fileSize} B`;
}

function formatTransferState(state: string): string {
  if (state === "done") {
    return "已发送";
  }
  if (state === "sending") {
    return "发送中";
  }
  if (state === "failed") {
    return "发送失败";
  }
  return state;
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
