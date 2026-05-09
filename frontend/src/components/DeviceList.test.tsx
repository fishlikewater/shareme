import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { PeerSummary } from "../lib/types";
import { DeviceList } from "./DeviceList";

type CollapsibleDeviceListProps = Parameters<typeof DeviceList>[0] & { collapsed?: boolean };

const CollapsibleDeviceList = DeviceList as (
  props: CollapsibleDeviceListProps,
) => ReturnType<typeof DeviceList>;

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

  it("收起时仍提供设备短名、状态文本和选中语义", () => {
    const peer: PeerSummary = {
      deviceId: "peer-1",
      deviceName: "办公室副机",
      trusted: true,
      online: true,
      reachable: true,
      agentTcpPort: 19090,
    };

    render(<CollapsibleDeviceList peers={[peer]} selectedPeerId="peer-1" collapsed onSelect={vi.fn()} />);

    const deviceButton = screen.getByRole("button", { name: /办公室副机/ });
    expect(deviceButton).toHaveAccessibleName(expect.stringContaining("已配对"));
    expect(deviceButton).toHaveAccessibleName(expect.stringMatching(/(?:^|[，、\s])可(?:立即)?发送/));
    expect(deviceButton).toHaveAttribute("aria-pressed", "true");
  });
});
