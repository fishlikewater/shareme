import { waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { createDesktopApiClient, notifyDesktopUiReady } from "./desktop-api";
import type { AgentEvent } from "./types";

describe("createDesktopApiClient", () => {
  it("replays backlog before switching to live desktop events", async () => {
    const unsubscribe = vi.fn();
    const commands = {
      Bootstrap: vi.fn().mockResolvedValue({
        localDeviceName: "desk-a",
        health: { status: "ok", discovery: "broadcast-ok", localAPIReady: true },
        peers: [],
        pairings: [],
        conversations: [],
        messages: [],
        transfers: [],
        eventSeq: 3,
      }),
      StartPairing: vi.fn().mockResolvedValue({ pairingId: "pair-1" }),
      ConfirmPairing: vi.fn().mockResolvedValue({ pairingId: "pair-1", status: "confirmed" }),
      SendText: vi.fn().mockResolvedValue({ messageId: "msg-1", body: "hello" }),
      SendFile: vi.fn().mockResolvedValue({
        transferId: "tx-1",
        messageId: "msg-1",
        fileName: "demo.txt",
        fileSize: 5,
        state: "sending",
        direction: "outgoing",
        bytesTransferred: 0,
        progressPercent: 0,
        rateBytesPerSec: 0,
        etaSeconds: null,
        active: true,
        createdAt: new Date().toISOString(),
      }),
      SendFilePath: vi.fn().mockResolvedValue({
        transferId: "tx-path-1",
        messageId: "msg-path-1",
        fileName: "drop.txt",
        fileSize: 5,
        state: "sending",
        direction: "outgoing",
        bytesTransferred: 0,
        progressPercent: 0,
        rateBytesPerSec: 0,
        etaSeconds: null,
        active: true,
        createdAt: new Date().toISOString(),
      }),
      PickLocalFile: vi.fn().mockResolvedValue({
        localFileId: "lf-1",
        displayName: "demo.txt",
        size: 5,
        acceleratedEligible: false,
      }),
      SendAcceleratedFile: vi.fn().mockResolvedValue({ transferId: "tx-2" }),
      ListMessageHistory: vi.fn().mockResolvedValue({
        conversationId: "conv-1",
        messages: [],
        hasMore: false,
      }),
      ReplayEvents: vi.fn().mockResolvedValue([
        { eventSeq: 4, kind: "peer.updated", payload: { deviceId: "peer-1" } },
      ]),
    };
    let eventHandler: ((event: AgentEvent) => void) | undefined;
    const eventsOn = vi.fn((_eventName: string, handler: (event: AgentEvent) => void) => {
      eventHandler = handler;
      handler({ eventSeq: 5, kind: "transfer.updated", payload: { transferId: "tx-1" } });
      return unsubscribe;
    });
    const eventsOff = vi.fn();

    const api = createDesktopApiClient({
      commands,
      eventsOn,
      eventsOff,
    });

    const snapshot = await api.bootstrap();
    expect(snapshot.localDeviceName).toBe("desk-a");

    await api.sendFile("peer-1");
    expect(commands.SendFile).toHaveBeenCalledWith("peer-1");
    await api.sendFilePath?.("peer-1", "C:\\tmp\\drop.txt");
    expect(commands.SendFilePath).toHaveBeenCalledWith("peer-1", "C:\\tmp\\drop.txt");

    const received: AgentEvent[] = [];
    const subscription = api.subscribeEvents({
      lastEventSeq: 3,
      onEvent(event) {
        received.push(event);
      },
    });

    expect(eventsOn).toHaveBeenCalledWith("shareme:event", expect.any(Function));
    expect(commands.ReplayEvents).toHaveBeenCalledWith(3);
    await waitFor(() => {
      expect(received.map((event) => event.eventSeq)).toEqual([4, 5]);
      expect(received.map((event) => event.kind)).toEqual(["peer.updated", "transfer.updated"]);
    });

    eventHandler?.({ eventSeq: 6, kind: "health.updated", payload: { status: "ok" } });
    expect(received[2]).toEqual({ eventSeq: 6, kind: "health.updated", payload: { status: "ok" } });

    subscription.close();
    expect(unsubscribe).toHaveBeenCalledTimes(1);
    expect(eventsOff).toHaveBeenCalledWith("shareme:event");
  });
});

describe("notifyDesktopUiReady", () => {
  const originalWindow = globalThis.window;

  afterEach(() => {
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: originalWindow,
    });
  });

  it("calls the desktop host ui ready hook when available", async () => {
    const uiReady = vi.fn().mockResolvedValue(undefined);

    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: {
        go: {
          main: {
            DesktopApp: {
              UiReady: uiReady,
            },
          },
        },
      },
    });

    await notifyDesktopUiReady();
    expect(uiReady).toHaveBeenCalledTimes(1);
  });
});
