import { afterEach, describe, expect, it, vi } from "vitest";

import type { AgentEvent } from "./types";

type FetchLike = typeof fetch;

class FakeEventSource {
  static instances: FakeEventSource[] = [];

  readonly close = vi.fn(() => {
    this.closed = true;
  });

  closed = false;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(readonly url: string) {
    FakeEventSource.instances.push(this);
  }

  emitMessage(event: AgentEvent) {
    this.onmessage?.(
      new MessageEvent("message", {
        data: JSON.stringify(event),
      }),
    );
  }

  emitError() {
    this.onerror?.(new Event("error"));
  }
}

function jsonResponse(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: {
      "Content-Type": "application/json",
    },
  });
}

describe("createLocalhostApiClient", () => {
  afterEach(() => {
    FakeEventSource.instances = [];
    vi.useRealTimers();
  });

  it("maps localhost JSON endpoints to the expected API routes", async () => {
    const fetchImpl = vi
      .fn<FetchLike>()
      .mockResolvedValueOnce(
        jsonResponse({
          localDeviceName: "Local Browser",
          health: { status: "ok", discovery: "broadcast-ok", localAPIReady: true },
          peers: [],
          pairings: [],
          conversations: [],
          messages: [],
          transfers: [],
          eventSeq: 4,
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          pairingId: "pair-1",
          peerDeviceId: "peer-1",
          peerDeviceName: "Peer One",
          shortCode: "123456",
          status: "pending",
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          pairingId: "pair-1",
          peerDeviceId: "peer-1",
          peerDeviceName: "Peer One",
          shortCode: "123456",
          status: "confirmed",
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          messageId: "msg-1",
          conversationId: "conv-peer-1",
          direction: "outgoing",
          kind: "text",
          body: "hello",
          status: "sent",
          createdAt: "2026-04-20T10:00:00Z",
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          localFileId: "lf-1",
          displayName: "archive.iso",
          size: 42,
          acceleratedEligible: true,
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          transferId: "tx-1",
          messageId: "msg-file-1",
          fileName: "archive.iso",
          fileSize: 42,
          state: "preparing",
          createdAt: "2026-04-20T10:01:00Z",
          direction: "outgoing",
          bytesTransferred: 0,
          progressPercent: 0,
          rateBytesPerSec: 0,
          etaSeconds: null,
          active: true,
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          conversationId: "conv-peer-1",
          messages: [],
          hasMore: true,
          nextCursor: "cursor-2",
        }),
      );

    const { createLocalhostApiClient } = await import("./localhost-api");
    const api = createLocalhostApiClient({
      fetch: fetchImpl,
      eventSource: FakeEventSource as unknown as typeof EventSource,
      location: new URL("http://127.0.0.1:18080/app"),
    });

    await api.bootstrap();
    await api.startPairing("peer-1");
    await api.confirmPairing("pair-1");
    await api.sendText("peer-1", "hello");
    await api.pickLocalFile();
    await api.sendAcceleratedFile("peer-1", "lf-1");
    await api.listMessageHistory("conv-peer-1", "cursor-1");

    expect(fetchImpl).toHaveBeenNthCalledWith(1, "http://127.0.0.1:18080/api/bootstrap", undefined);
    expect(fetchImpl).toHaveBeenNthCalledWith(2, "http://127.0.0.1:18080/api/pairings/start", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ peerDeviceId: "peer-1" }),
    });
    expect(fetchImpl).toHaveBeenNthCalledWith(3, "http://127.0.0.1:18080/api/pairings/confirm", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ pairingId: "pair-1" }),
    });
    expect(fetchImpl).toHaveBeenNthCalledWith(4, "http://127.0.0.1:18080/api/messages/text", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ peerDeviceId: "peer-1", body: "hello" }),
    });
    expect(fetchImpl).toHaveBeenNthCalledWith(5, "http://127.0.0.1:18080/api/local-files/pick", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    });
    expect(fetchImpl).toHaveBeenNthCalledWith(6, "http://127.0.0.1:18080/api/transfers/accelerated", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ peerDeviceId: "peer-1", localFileId: "lf-1" }),
    });
    expect(fetchImpl).toHaveBeenNthCalledWith(
      7,
      "http://127.0.0.1:18080/api/messages/history?conversationId=conv-peer-1&beforeCursor=cursor-1",
      undefined,
    );
  });

  it("uploads normal files with browser multipart form data", async () => {
    const fetchImpl = vi.fn<FetchLike>().mockResolvedValue(
      jsonResponse({
        transferId: "tx-1",
        messageId: "msg-file-1",
        fileName: "demo.txt",
        fileSize: 5,
        state: "sending",
        createdAt: "2026-04-20T10:01:00Z",
        direction: "outgoing",
        bytesTransferred: 0,
        progressPercent: 0,
        rateBytesPerSec: 0,
        etaSeconds: null,
        active: true,
      }),
    );

    const file = new File(["hello"], "demo.txt", { type: "text/plain" });
    const input = document.createElement("input");
    Object.defineProperty(input, "files", {
      configurable: true,
      value: [file],
    });
    Object.defineProperty(input, "click", {
      configurable: true,
      value: () => {
        input.dispatchEvent(new Event("change"));
      },
    });

    const { createLocalhostApiClient } = await import("./localhost-api");
    const api = createLocalhostApiClient({
      fetch: fetchImpl,
      eventSource: FakeEventSource as unknown as typeof EventSource,
      location: new URL("http://localhost:18080/app"),
      createFileInput: () => input,
    });

    await api.sendFile("peer-1");

    expect(fetchImpl).toHaveBeenCalledTimes(1);
    const [url, options] = fetchImpl.mock.calls[0];
    expect(url).toBe("http://localhost:18080/api/transfers/file");
    expect(options?.method).toBe("POST");
    expect(options?.body).toBeInstanceOf(FormData);
    expect(options?.headers).toBeUndefined();

    const form = options?.body as FormData;
    expect(form.get("peerDeviceId")).toBe("peer-1");
    expect(form.get("file")).toBe(file);
  });

  it("deduplicates SSE events by eventSeq and reconnects from the latest cursor", async () => {
    vi.useFakeTimers();

    const { createLocalhostApiClient } = await import("./localhost-api");
    const api = createLocalhostApiClient({
      fetch: vi.fn<FetchLike>(),
      eventSource: FakeEventSource as unknown as typeof EventSource,
      location: new URL("http://127.0.0.1:18080/app"),
      reconnectDelayMs: 1000,
    });

    const received: AgentEvent[] = [];
    const subscription = api.subscribeEvents({
      lastEventSeq: 5,
      onEvent: (event) => {
        received.push(event);
      },
    });

    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0]?.url).toBe("http://127.0.0.1:18080/api/events?afterSeq=5");

    FakeEventSource.instances[0]?.emitMessage({
      eventSeq: 6,
      kind: "peer.updated",
      payload: { deviceId: "peer-1" },
    });
    FakeEventSource.instances[0]?.emitMessage({
      eventSeq: 6,
      kind: "peer.updated",
      payload: { deviceId: "peer-1", duplicate: true },
    });
    FakeEventSource.instances[0]?.emitMessage({
      eventSeq: 4,
      kind: "peer.updated",
      payload: { deviceId: "peer-old" },
    });

    expect(received.map((event) => event.eventSeq)).toEqual([6]);

    FakeEventSource.instances[0]?.emitError();
    await vi.advanceTimersByTimeAsync(1000);

    expect(FakeEventSource.instances).toHaveLength(2);
    expect(FakeEventSource.instances[0]?.close).toHaveBeenCalledTimes(1);
    expect(FakeEventSource.instances[1]?.url).toBe("http://127.0.0.1:18080/api/events?afterSeq=6");

    FakeEventSource.instances[1]?.emitMessage({
      eventSeq: 7,
      kind: "transfer.updated",
      payload: { transferId: "tx-1" },
    });
    expect(received.map((event) => event.eventSeq)).toEqual([6, 7]);

    subscription.reconnect();

    expect(FakeEventSource.instances).toHaveLength(3);
    expect(FakeEventSource.instances[1]?.close).toHaveBeenCalledTimes(1);
    expect(FakeEventSource.instances[2]?.url).toBe("http://127.0.0.1:18080/api/events?afterSeq=7");

    subscription.close();
    expect(FakeEventSource.instances[2]?.close).toHaveBeenCalledTimes(1);
  });
});
