import { act, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import App from "./App";
import type { LocalApi } from "./lib/api";
import type { AgentEvent, BootstrapSnapshot } from "./lib/types";

async function flushRender(cycles = 3) {
  for (let index = 0; index < cycles; index += 1) {
    await act(async () => {
      await Promise.resolve();
    });
  }
}

afterEach(() => {
  vi.useRealTimers();
});

describe("App refresh", () => {
  it("periodically reloads bootstrap so non-event state changes are eventually reflected", async () => {
    vi.useFakeTimers();

    const first: BootstrapSnapshot = {
      localDeviceName: "Local One",
      health: {
        status: "ok",
        discovery: "broadcast-ok",
        localAPIReady: true,
        agentPort: 19090,
      },
      peers: [],
      pairings: [],
      conversations: [],
      messages: [],
      transfers: [],
      eventSeq: 1,
    };
    const second: BootstrapSnapshot = {
      ...first,
      localDeviceName: "Local Two",
      eventSeq: 2,
    };

    const api: LocalApi = {
      bootstrap: vi.fn().mockResolvedValueOnce(first).mockResolvedValue(second),
      startPairing: vi.fn(),
      confirmPairing: vi.fn(),
      sendText: vi.fn(),
      sendFile: vi.fn(),
      pickLocalFile: vi.fn(),
      sendAcceleratedFile: vi.fn(),
      listMessageHistory: vi.fn(),
      subscribeEvents: vi.fn(() => ({
        close: vi.fn(),
        reconnect: vi.fn(),
      })),
    };

    render(<App api={api} />);

    await flushRender();
    expect(screen.getAllByText("Local One").length).toBeGreaterThan(0);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    await flushRender();
    expect(screen.getAllByText("Local Two").length).toBeGreaterThan(0);
  });

  it("keeps newer realtime updates when a stale bootstrap response arrives later", async () => {
    vi.useFakeTimers();

    const initial: BootstrapSnapshot = {
      localDeviceName: "Local One",
      health: {
        status: "ok",
        discovery: "broadcast-ok",
        localAPIReady: true,
        agentPort: 19090,
      },
      peers: [
        {
          deviceId: "peer-1",
          deviceName: "Peer One",
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
          peerDeviceName: "Peer One",
        },
      ],
      messages: [],
      transfers: [],
      eventSeq: 1,
    };

    let onEvent: ((event: AgentEvent) => void) | undefined;
    const api: LocalApi = {
      bootstrap: vi.fn().mockResolvedValueOnce(initial).mockResolvedValue(initial),
      startPairing: vi.fn(),
      confirmPairing: vi.fn(),
      sendText: vi.fn(),
      sendFile: vi.fn(),
      pickLocalFile: vi.fn(),
      sendAcceleratedFile: vi.fn(),
      listMessageHistory: vi.fn(),
      subscribeEvents: vi.fn((options) => {
        onEvent = options.onEvent;
        return {
          close: vi.fn(),
          reconnect: vi.fn(),
        };
      }),
    };

    render(<App api={api} />);
    await flushRender();

    act(() => {
      onEvent?.({
        eventSeq: 2,
        kind: "message.upserted",
        payload: {
          messageId: "msg-live",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "live-body",
          status: "sent",
          createdAt: "2026-04-17T11:00:00Z",
        },
      });
    });

    const messageList = screen.getByTestId("message-list");
    expect(within(messageList).getByText("live-body")).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    await flushRender();

    expect(within(messageList).getByText("live-body")).toBeInTheDocument();
  });

  it("re-subscribes to realtime events after the first bootstrap recovers on refresh", async () => {
    vi.useFakeTimers();

    const recovered: BootstrapSnapshot = {
      localDeviceName: "Recovered Local",
      health: {
        status: "ok",
        discovery: "broadcast-ok",
        localAPIReady: true,
        agentPort: 19090,
      },
      peers: [
        {
          deviceId: "peer-1",
          deviceName: "Peer One",
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
          peerDeviceName: "Peer One",
        },
      ],
      messages: [],
      transfers: [],
      eventSeq: 3,
    };

    let onEvent: ((event: AgentEvent) => void) | undefined;
    const subscribeEvents = vi.fn((options: { lastEventSeq?: number; onEvent: (event: AgentEvent) => void }) => {
      onEvent = options.onEvent;
      return {
        close: vi.fn(),
        reconnect: vi.fn(),
      };
    });

    const api: LocalApi = {
      bootstrap: vi.fn().mockRejectedValueOnce(new Error("ECONNREFUSED")).mockResolvedValue(recovered),
      startPairing: vi.fn(),
      confirmPairing: vi.fn(),
      sendText: vi.fn(),
      sendFile: vi.fn(),
      pickLocalFile: vi.fn(),
      sendAcceleratedFile: vi.fn(),
      listMessageHistory: vi.fn(),
      subscribeEvents,
    };

    render(<App api={api} />);

    await flushRender();
    expect(screen.getByText("无法连接本机代理")).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    await flushRender();

    expect(screen.getAllByText("Recovered Local").length).toBeGreaterThan(0);
    expect(subscribeEvents).toHaveBeenCalledTimes(1);

    act(() => {
      onEvent?.({
        eventSeq: 4,
        kind: "message.upserted",
        payload: {
          messageId: "msg-after-recover",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "after-recover",
          status: "sent",
          createdAt: "2026-04-17T11:05:00Z",
        },
      });
    });

    expect(within(screen.getByTestId("message-list")).getByText("after-recover")).toBeInTheDocument();
  });
});
