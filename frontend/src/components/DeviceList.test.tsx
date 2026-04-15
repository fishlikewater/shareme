import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { PeerSummary } from "../lib/types";
import { DeviceList } from "./DeviceList";

describe("DeviceList", () => {
  it("在广播暂未恢复时仍提示可继续直连", () => {
    const peer: PeerSummary = {
      deviceId: "peer-1",
      deviceName: "会议室电脑",
      trusted: true,
      online: false,
      reachable: true,
      agentTcpPort: 19090,
    };

    render(<DeviceList peers={[peer]} onSelect={vi.fn()} />);

    expect(screen.getByText("已配对，可继续直连")).toBeInTheDocument();
    expect(screen.queryByText("已配对，但当前离线")).not.toBeInTheDocument();
  });

  it("在线但不可达时不误报设备离线", () => {
    const peer: PeerSummary = {
      deviceId: "peer-2",
      deviceName: "会议室电脑",
      trusted: true,
      online: true,
      reachable: false,
      agentTcpPort: 19090,
    };

    render(<DeviceList peers={[peer]} onSelect={vi.fn()} />);

    expect(screen.getByText("已发现，但暂时无法直连")).toBeInTheDocument();
    expect(screen.queryByText("设备当前不在线，暂时不能建立直连传输")).not.toBeInTheDocument();
  });
});
