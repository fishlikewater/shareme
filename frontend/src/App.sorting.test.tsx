import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import App from "./App";
import type { LocalApi } from "./lib/api";
import type { BootstrapSnapshot } from "./lib/types";

const snapshot: BootstrapSnapshot = {
  localDeviceName: "local-device",
  health: {
    status: "ok",
    discovery: "broadcast-ok",
    localAPIReady: true,
    agentPort: 19090,
  },
  peers: [
    {
      deviceId: "peer-1",
      deviceName: "Alpha Peer",
      trusted: true,
      online: true,
      reachable: true,
      agentTcpPort: 19090,
    },
  ],
  pairings: [],
  conversations: [
    {
      conversationId: "conv-peer-1",
      peerDeviceId: "peer-1",
      peerDeviceName: "Alpha Peer",
    },
  ],
  messages: [
    {
      messageId: "msg-older",
      conversationId: "conv-peer-1",
      direction: "incoming",
      kind: "text",
      body: "older message",
      status: "sent",
      createdAt: "2026-04-10T08:00:00Z",
    },
    {
      messageId: "msg-newer",
      conversationId: "conv-peer-1",
      direction: "incoming",
      kind: "text",
      body: "newer message",
      status: "sent",
      createdAt: "2026-04-10T08:00:00.1Z",
    },
  ],
  transfers: [],
  eventSeq: 7,
};

const api: LocalApi = {
  bootstrap: vi.fn().mockResolvedValue(snapshot),
  startPairing: vi.fn(),
  confirmPairing: vi.fn(),
  sendText: vi.fn(),
  sendFile: vi.fn(),
  pickLocalFile: vi.fn(),
  sendAcceleratedFile: vi.fn(),
  subscribeEvents: vi.fn(() => ({
    close: vi.fn(),
    reconnect: vi.fn(),
  })),
};

describe("App sorting", () => {
  it("uses parsed timestamps for preview and conversation ordering", async () => {
    render(<App api={api} />);

    const peerButton = await screen.findByRole("button", { name: /Alpha Peer/ });
    expect(peerButton).toHaveTextContent("newer message");

    const messageCards = document.querySelectorAll(".ms-message-list .ms-message-card");
    expect(messageCards).toHaveLength(2);
    expect(messageCards[0]).toHaveTextContent("older message");
    expect(messageCards[1]).toHaveTextContent("newer message");
  });
});
