import { useRef, useState, type ChangeEvent, type FormEvent } from "react";

import { FileMessageCard } from "./FileMessageCard";
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
          <span className="ms-eyebrow">沟通中心</span>
          <h2 className="ms-chat-title">
            {peer ? `连接 ${peer.deviceName}` : "请选择一个可信设备开始交流"}
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
            <strong>需要一个伙伴来开启共享旅程</strong>
            <p>在左侧列表中选择设备，完成验证后即可互传文本和文件。</p>
          </div>
          <div className="ms-workspace-steps" aria-label="快速引导">
            <div className="ms-workspace-step">
              <span className="ms-workspace-step__index">1</span>
              <div>
                <strong>选择设备</strong>
                <p>点击目标设备卡片，查看详情并发起连接。</p>
              </div>
            </div>
            <div className="ms-workspace-step">
              <span className="ms-workspace-step__index">2</span>
              <div>
                <strong>确认配对</strong>
                <p>输入配对码或接受邀请，完成设备信任流程。</p>
              </div>
            </div>
            <div className="ms-workspace-step">
              <span className="ms-workspace-step__index">3</span>
              <div>
                <strong>开始传输</strong>
                <p>文字、图片与文件都可以轻松分享。</p>
              </div>
            </div>
          </div>
        </section>
      ) : null}
      {peer && !peer.trusted ? (
        <div className="ms-chat-blocker">尚未完成信任，请先授权后再传输内容。</div>
      ) : null}
      {peer && peer.trusted && !peer.reachable ? (
        <div className="ms-chat-blocker">当前设备暂时无法访问，请稍后重试。</div>
      ) : null}

      {canSend ? (
        <>
          <div className="ms-section-head">
            <span className="ms-section-title">实时沟通</span>
            <span className="ms-section-hint">可发送文本与文件</span>
          </div>

          <div className="ms-message-list">
            {messages.length === 0 ? (
              <div className="ms-empty-card">还没有消息，快来发送第一条吧。</div>
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
              placeholder="输入消息或者点击下方按钮选择文件"
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
                {sendingFile ? "文件上传中..." : "选择文件"}
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
    return "请选择一个设备，在列表中查看状态并建立连接。";
  }
  if (!peer.trusted) {
    return "此设备暂未信任，完成配对后才可安全通讯。";
  }
  if (!peer.reachable) {
    return "设备已信任但当前不可达，请稍后重试。";
  }
  return "连接准备就绪，开始发送文本或文件吧。";
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
