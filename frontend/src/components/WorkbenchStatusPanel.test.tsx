import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { HealthSnapshot, TransferSnapshot } from "../lib/types";
import { WorkbenchStatusPanel } from "./WorkbenchStatusPanel";

const health: HealthSnapshot = {
  status: "ok",
  discovery: "broadcast-ok",
  localAPIReady: true,
  agentPort: 19090,
  issues: [],
};

const activeTransfer: TransferSnapshot = {
  transferId: "transfer-1",
  messageId: "msg-1",
  fileName: "demo.pdf",
  fileSize: 2_097_152,
  state: "sending",
  createdAt: "2026-05-09T10:00:00Z",
  direction: "outgoing",
  bytesTransferred: 1_048_576,
  progressPercent: 50,
  rateBytesPerSec: 512_000,
  etaSeconds: 4,
  active: true,
};

describe("WorkbenchStatusPanel", () => {
  it("正常且无传输时以摘要呈现", () => {
    render(<WorkbenchStatusPanel health={health} lastEventSeq={7} transfers={[]} />);

    const panel = screen.getByRole("complementary", { name: "运行状态" });
    expect(within(panel).getByText("本机服务已就绪")).toBeInTheDocument();
    expect(within(panel).queryByLabelText("传输横幅")).not.toBeInTheDocument();
  });

  it("有活跃传输时自动展示传输摘要", () => {
    render(<WorkbenchStatusPanel health={health} lastEventSeq={7} transfers={[activeTransfer]} />);

    expect(screen.getByLabelText("传输横幅")).toBeInTheDocument();
    expect(screen.getByText("demo.pdf")).toBeInTheDocument();
  });

  it("允许用户展开和收起详情", () => {
    render(<WorkbenchStatusPanel health={health} lastEventSeq={7} transfers={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "展开运行状态" }));
    expect(screen.getByRole("region", { name: "连接状态" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "本机服务已就绪", level: 3 })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "收起运行状态" })).toBeInTheDocument();
  });
});
