import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ComponentProps } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ChatPane } from "./ChatPane";
import type { ConversationMessage, PeerSummary } from "../lib/types";

const trustedPeer: PeerSummary = {
  deviceId: "peer-1",
  deviceName: "办公电脑",
  trusted: true,
  online: true,
  reachable: true,
  agentTcpPort: 19090,
};

function renderTrustedChatPane(
  props: Partial<ComponentProps<typeof ChatPane>> = {},
): ComponentProps<typeof ChatPane> {
  const defaults: ComponentProps<typeof ChatPane> = {
    peer: trustedPeer,
    messages: [],
    sendingText: false,
    sendingFile: false,
    pickingLocalFile: false,
    sendingAcceleratedFile: false,
    pickedLocalFile: null,
    historyHasMore: false,
    historyLoading: false,
    onSendText: vi.fn(),
    onSendFile: vi.fn().mockResolvedValue(undefined),
    onPickLocalFile: vi.fn(),
    onSendAcceleratedFile: vi.fn(),
    onLoadOlderMessages: vi.fn(),
  };
  const mergedProps = { ...defaults, ...props };
  render(<ChatPane {...mergedProps} />);
  return mergedProps;
}

const textMessage: ConversationMessage = {
  messageId: "msg-text-1",
  conversationId: "conv-peer-1",
  direction: "incoming",
  kind: "text",
  body: "复制这段文本",
  status: "sent",
  createdAt: "2026-04-10T08:00:00Z",
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ChatPane", () => {
  it("普通文件发送按钮直接调用桌面命令，不再依赖隐藏 file input", async () => {
    const onSendFile = vi.fn().mockResolvedValue(undefined);

    renderTrustedChatPane({ onSendFile });

    expect(screen.getByRole("heading", { name: trustedPeer.deviceName })).toBeInTheDocument();
    expect(screen.getByLabelText("当前设备状态")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "选择文件" }));

    expect(onSendFile).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("file-input")).not.toBeInTheDocument();
  });

  it("文本消息右上角提供复制按钮，点击后写入剪贴板", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    renderTrustedChatPane({ messages: [textMessage] });

    const copyButton = screen.getByRole("button", { name: "复制消息文本" });
    expect(copyButton).toHaveClass("ms-message-copy-button");
    expect(copyButton).not.toHaveTextContent("复制");
    const icon = copyButton.querySelector(".ms-copy-icon");
    expect(icon).toBeInTheDocument();
    expect(icon?.tagName.toLowerCase()).toBe("svg");

    fireEvent.click(copyButton);

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(textMessage.body);
    });
    expect(screen.getByRole("status", { name: "复制结果" })).toHaveTextContent("已复制");
  });

  it("在消息输入框按 Enter 直接发送文字并清空草稿", async () => {
    const onSendText = vi.fn().mockResolvedValue(undefined);
    renderTrustedChatPane({ onSendText });

    const input = screen.getByRole("textbox", { name: "消息输入框" });
    fireEvent.change(input, { target: { value: "你好" } });
    fireEvent.keyDown(input, { key: "Enter", code: "Enter" });

    await waitFor(() => {
      expect(onSendText).toHaveBeenCalledWith("你好");
    });
    await waitFor(() => {
      expect(input).toHaveValue("");
    });
  });

  it("Shift+Enter 保留为输入换行，不触发发送", () => {
    const onSendText = vi.fn().mockResolvedValue(undefined);
    renderTrustedChatPane({ onSendText });

    const input = screen.getByRole("textbox", { name: "消息输入框" });
    fireEvent.change(input, { target: { value: "第一行" } });
    fireEvent.keyDown(input, { key: "Enter", code: "Enter", shiftKey: true });

    expect(onSendText).not.toHaveBeenCalled();
  });

  it("外部文件拖入消息输入框时直接发送该文件", async () => {
    const onSendFile = vi.fn().mockResolvedValue(undefined);
    const file = new File(["hello"], "hello.txt", { type: "text/plain" });
    renderTrustedChatPane({ onSendFile });

    fireEvent.drop(screen.getByRole("textbox", { name: "消息输入框" }), {
      dataTransfer: { files: [file] },
    });

    await waitFor(() => {
      expect(onSendFile).toHaveBeenCalledWith(file);
    });
  });
});
