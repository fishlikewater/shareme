import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ConversationMessage, TransferSnapshot } from "../lib/types";
import { FileMessageCard } from "./FileMessageCard";

const message: ConversationMessage = {
  messageId: "msg-file",
  conversationId: "conv-peer-1",
  direction: "outgoing",
  kind: "file",
  body: "report.docx",
  status: "sending",
  createdAt: "2026-01-01T00:00:00Z",
};

const transfer: TransferSnapshot = {
  transferId: "transfer-file",
  messageId: "msg-file",
  fileName: "report.docx",
  fileSize: 2_097_152,
  state: "sending",
  createdAt: "2026-01-01T00:00:00Z",
  direction: "outgoing",
  bytesTransferred: 1_048_576,
  progressPercent: 50,
  rateBytesPerSec: 512_000,
  etaSeconds: 10,
  active: true,
};

describe("FileMessageCard", () => {
  it("展示进度统计、速率和 ETA", () => {
    render(<FileMessageCard message={message} transfer={transfer} />);

    expect(screen.getByText("report.docx")).toBeInTheDocument();
    expect(screen.getByText("发送")).toBeInTheDocument();
    expect(screen.getByText("50%")).toBeInTheDocument();
    expect(screen.getByText("1 MB / 2 MB")).toBeInTheDocument();
    expect(screen.getByText("速率 500 KB/s")).toBeInTheDocument();
    expect(screen.getByText("ETA 00:10")).toBeInTheDocument();
    expect(screen.getByText("传输中")).toBeInTheDocument();
  });

  it("展示入站文件的接收中文案", () => {
    render(
      <FileMessageCard
        message={{ ...message, direction: "incoming" }}
        transfer={{ ...transfer, direction: "incoming", state: "receiving" }}
      />,
    );

    expect(screen.getByText("接收")).toBeInTheDocument();
    expect(screen.getByText("接收中")).toBeInTheDocument();
  });

  it("未知 ETA 时不展示 ETA，并为进度条提供可访问语义", () => {
    render(<FileMessageCard message={message} transfer={{ ...transfer, etaSeconds: null }} />);

    expect(screen.queryByText(/^ETA /)).not.toBeInTheDocument();

    const progressbar = screen.getByRole("progressbar", {
      name: "report.docx 传输进度",
    });
    expect(progressbar).toHaveAttribute("aria-valuemin", "0");
    expect(progressbar).toHaveAttribute("aria-valuemax", "100");
    expect(progressbar).toHaveAttribute("aria-valuenow", "50");
    expect(progressbar.getAttribute("aria-valuetext")).toContain("50%");
    expect(progressbar.getAttribute("aria-valuetext")).toContain("1 MB / 2 MB");
    expect(progressbar.getAttribute("aria-valuetext")).not.toContain("ETA");
  });

  it("完成态和失败态不展示 ETA", () => {
    const { rerender } = render(
      <FileMessageCard
        message={{ ...message, status: "done" }}
        transfer={{
          ...transfer,
          state: "done",
          progressPercent: 100,
          bytesTransferred: transfer.fileSize,
          etaSeconds: 12,
        }}
      />,
    );

    expect(screen.queryByText(/^ETA /)).not.toBeInTheDocument();

    rerender(
      <FileMessageCard
        message={{ ...message, status: "failed" }}
        transfer={{ ...transfer, state: "failed", etaSeconds: 0 }}
      />,
    );

    expect(screen.queryByText(/^ETA /)).not.toBeInTheDocument();
  });

  it("rounds displayed percent while keeping precise progress width", () => {
    const { container } = render(
      <FileMessageCard message={message} transfer={{ ...transfer, progressPercent: 50.6 }} />,
    );

    expect(screen.getByText("51%")).toBeInTheDocument();
    expect(screen.queryByText("50.6%")).not.toBeInTheDocument();
    const progressBar = container.querySelector(".ms-file-card__progress-bar");
    expect(progressBar).not.toBeNull();
    expect(progressBar).toHaveStyle("width: 50.6%");
  });

  it("caps in-flight display percent at 99 until transfer is done", () => {
    const { container } = render(
      <FileMessageCard
        message={message}
        transfer={{ ...transfer, progressPercent: 99.6, state: "sending" }}
      />,
    );

    expect(screen.getByText("99%")).toBeInTheDocument();
    expect(screen.queryByText("100%")).not.toBeInTheDocument();
    const progressBar = container.querySelector(".ms-file-card__progress-bar");
    expect(progressBar).not.toBeNull();
    expect(progressBar).toHaveStyle("width: 99.6%");
  });

  it("缺少 transfer 快照时不伪造零进度和零速率", () => {
    render(<FileMessageCard message={{ ...message, status: "sent" }} />);

    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
    expect(screen.queryByText("0%")).not.toBeInTheDocument();
    expect(screen.queryByText("0 B / 0 B")).not.toBeInTheDocument();
    expect(screen.queryByText("速率 0 B/s")).not.toBeInTheDocument();
    expect(screen.queryByText("sent")).not.toBeInTheDocument();
    expect(screen.getByText("已发送")).toBeInTheDocument();
  });

  it("telemetry 尚未预热时不伪造 0% 和 0 B/s", () => {
    render(
      <FileMessageCard
        message={message}
        transfer={{
          ...transfer,
          bytesTransferred: 0,
          progressPercent: 0,
          rateBytesPerSec: 0,
          etaSeconds: null,
        }}
      />,
    );

    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
    expect(screen.queryByText("0%")).not.toBeInTheDocument();
    expect(screen.queryByText("0 B / 2 MB")).not.toBeInTheDocument();
    expect(screen.queryByText("速率 0 B/s")).not.toBeInTheDocument();
    expect(screen.getByText("传输中")).toBeInTheDocument();
  });
});
