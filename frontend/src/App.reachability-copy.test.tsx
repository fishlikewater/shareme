import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import App from "./App";
import type { LocalApi } from "./lib/api";
import type {
  AgentEvent,
  BootstrapSnapshot,
  LocalFileSnapshot,
  MessageSnapshot,
  PairingSnapshot,
  TransferSnapshot,
} from "./lib/types";

function createApi(snapshot: BootstrapSnapshot): LocalApi {
  return {
    bootstrap: vi.fn().mockResolvedValue(snapshot),
    startPairing: vi.fn<(peerDeviceId: string) => Promise<PairingSnapshot>>(),
    confirmPairing: vi.fn<(pairingId: string) => Promise<PairingSnapshot>>(),
    sendText: vi.fn<(peerDeviceId: string, body: string) => Promise<MessageSnapshot>>(),
    sendFile: vi.fn<(peerDeviceId: string, file?: File) => Promise<TransferSnapshot>>(),
    pickLocalFile: vi.fn<() => Promise<LocalFileSnapshot>>(),
    sendAcceleratedFile: vi.fn<(peerDeviceId: string, localFileId: string) => Promise<TransferSnapshot>>(),
    listMessageHistory: vi.fn(),
    subscribeEvents: vi.fn(
      (_options: { lastEventSeq?: number; onEvent: (event: AgentEvent) => void }) => ({
        close: vi.fn(),
        reconnect: vi.fn(),
      }),
    ),
  };
}

describe("App reachability copy", () => {
  it("把 ready 指标和设备文案都对齐到可连接语义", async () => {
    const snapshot: BootstrapSnapshot = {
      localDeviceName: "我的电脑",
      health: {
        status: "ok",
        discovery: "broadcast-ok",
        localAPIReady: true,
        agentPort: 19090,
      },
      peers: [
        {
          deviceId: "peer-1",
          deviceName: "会议室电脑",
          trusted: true,
          online: false,
          reachable: true,
          agentTcpPort: 19090,
        },
      ],
      pairings: [],
      conversations: [],
      messages: [],
      transfers: [],
      eventSeq: 1,
    };

    render(<App api={createApi(snapshot)} />);

    expect(await screen.findByText("已信任且可连接")).toBeInTheDocument();
    expect(screen.getByText("已配对，可继续直连")).toBeInTheDocument();
  });
});
