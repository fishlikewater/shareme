import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ChatPane } from "./ChatPane";
import type { PeerSummary } from "../lib/types";

const trustedPeer: PeerSummary = {
  deviceId: "peer-1",
  deviceName: "办公电脑",
  trusted: true,
  online: true,
  reachable: true,
  agentTcpPort: 19090,
};

describe("ChatPane", () => {
  it("普通文件发送按钮直接调用桌面命令，不再依赖隐藏 file input", async () => {
    const onSendFile = vi.fn().mockResolvedValue(undefined);

    render(
      <ChatPane
        peer={trustedPeer}
        messages={[]}
        sendingText={false}
        sendingFile={false}
        pickingLocalFile={false}
        sendingAcceleratedFile={false}
        pickedLocalFile={null}
        historyHasMore={false}
        historyLoading={false}
        onSendText={vi.fn()}
        onSendFile={onSendFile}
        onPickLocalFile={vi.fn()}
        onSendAcceleratedFile={vi.fn()}
        onLoadOlderMessages={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "选择文件" }));

    expect(onSendFile).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("file-input")).not.toBeInTheDocument();
  });
});
