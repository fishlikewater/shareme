import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { TransferSnapshot } from "../lib/types";
import { TransferStatusBanner } from "./TransferStatusBanner";

const transfers: TransferSnapshot[] = [
  {
    transferId: "transfer-banner",
    messageId: "msg-banner",
    fileName: "demo.pdf",
    fileSize: 2_097_152,
    state: "receiving",
    createdAt: "2026-01-01T00:00:00Z",
    direction: "incoming",
    bytesTransferred: 1_048_576,
    progressPercent: 50,
    rateBytesPerSec: 512_000,
    etaSeconds: 10,
    active: true,
  },
];

describe("TransferStatusBanner", () => {
  it("展示活动传输卡片", () => {
    render(<TransferStatusBanner transfers={transfers} />);

    expect(screen.getByText("文件传输")).toBeInTheDocument();
    expect(screen.getByText("demo.pdf")).toBeInTheDocument();
    expect(screen.getByText("接收")).toBeInTheDocument();
    expect(screen.getByText("50%")).toBeInTheDocument();
    expect(screen.getByText("1 MB / 2 MB")).toBeInTheDocument();
    expect(screen.getByText("速率 500 KB/s")).toBeInTheDocument();
    expect(screen.getByText("ETA 00:10")).toBeInTheDocument();
    expect(screen.getByText("接收中")).toBeInTheDocument();
  });

  it("当没有活动传输时不渲染", () => {
    const { container } = render(<TransferStatusBanner transfers={[]} />);
    expect(container.firstChild).toBeNull();
  });

  it("未知 ETA 时不展示 ETA，并为横幅进度条提供可访问语义", () => {
    render(<TransferStatusBanner transfers={[{ ...transfers[0], etaSeconds: null }]} />);

    expect(screen.queryByText(/^ETA /)).not.toBeInTheDocument();

    const progressbar = screen.getByRole("progressbar", {
      name: "demo.pdf 传输进度",
    });
    expect(progressbar).toHaveAttribute("aria-valuemin", "0");
    expect(progressbar).toHaveAttribute("aria-valuemax", "100");
    expect(progressbar).toHaveAttribute("aria-valuenow", "50");
    expect(progressbar.getAttribute("aria-valuetext")).toContain("50%");
    expect(progressbar.getAttribute("aria-valuetext")).toContain("1 MB / 2 MB");
    expect(progressbar.getAttribute("aria-valuetext")).not.toContain("ETA");
  });

  it("完成态和失败态的横幅不展示 ETA", () => {
    const { rerender } = render(
      <TransferStatusBanner
        transfers={[
          {
            ...transfers[0],
            state: "done",
            progressPercent: 100,
            bytesTransferred: transfers[0].fileSize,
            etaSeconds: 8,
          },
        ]}
      />,
    );

    expect(screen.queryByText(/^ETA /)).not.toBeInTheDocument();

    rerender(<TransferStatusBanner transfers={[{ ...transfers[0], state: "failed", etaSeconds: 0 }]} />);

    expect(screen.queryByText(/^ETA /)).not.toBeInTheDocument();
  });

  it("rounds displayed percent while keeping precise banner width", () => {
    const { container } = render(
      <TransferStatusBanner transfers={[{ ...transfers[0], progressPercent: 50.6 }]} />,
    );

    expect(screen.getByText("51%")).toBeInTheDocument();
    expect(screen.queryByText("50.6%")).not.toBeInTheDocument();
    const progressFill = container.querySelector(".ms-transfer-banner__progress-fill");
    expect(progressFill).not.toBeNull();
    expect(progressFill).toHaveStyle("width: 50.6%");
  });

  it("caps active banner percent at 99 until the transfer is done", () => {
    const { container } = render(
      <TransferStatusBanner
        transfers={[{ ...transfers[0], progressPercent: 99.6, state: "receiving" }]}
      />,
    );

    expect(screen.getByText("99%")).toBeInTheDocument();
    expect(screen.queryByText("100%")).not.toBeInTheDocument();
    const progressFill = container.querySelector(".ms-transfer-banner__progress-fill");
    expect(progressFill).not.toBeNull();
    expect(progressFill).toHaveStyle("width: 99.6%");
  });

  it("does not announce the whole high-frequency banner as a live region", () => {
    render(<TransferStatusBanner transfers={transfers} />);

    expect(screen.getByLabelText("传输横幅")).not.toHaveAttribute("aria-live");
  });

  it("telemetry 尚未预热时不伪造 0% 和 0 B/s", () => {
    render(
      <TransferStatusBanner
        transfers={[
          {
            ...transfers[0],
            bytesTransferred: 0,
            progressPercent: 0,
            rateBytesPerSec: 0,
            etaSeconds: null,
          },
        ]}
      />,
    );

    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
    expect(screen.queryByText("0%")).not.toBeInTheDocument();
    expect(screen.queryByText("0 B / 2 MB")).not.toBeInTheDocument();
    expect(screen.queryByText("速率 0 B/s")).not.toBeInTheDocument();
    expect(screen.getByText("接收中")).toBeInTheDocument();
  });
});
