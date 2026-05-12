import { describe, expect, it, vi } from "vitest";

import type { AgentEvent } from "./types";

class MockEventSource {
  public onmessage: ((event: MessageEvent<string>) => void) | null = null;
  public onerror: ((event: Event) => void) | null = null;
  public closed = false;

  public constructor(public readonly url: string) {}

  emit(payload: AgentEvent) {
    this.onmessage?.(new MessageEvent("message", { data: JSON.stringify(payload) }));
  }

  fail() {
    this.onerror?.(new Event("error"));
  }

  close() {
    this.closed = true;
  }
}

describe("createLocalhostApiClient", () => {
  it("将 localhost API 映射到约定的 endpoints", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({}),
      text: async () => "",
    });
    const { createLocalhostApiClient } = await import("./localhost-api");
    const api = createLocalhostApiClient({
      origin: "http://127.0.0.1:52350",
      fetchFn,
      createEventSource: vi.fn(),
      pickFile: vi.fn(),
    });

    await api.bootstrap();
    await api.startPairing("peer-1");
    await api.confirmPairing("pair-1");
    await api.sendText("peer-1", "hello");
    await api.pickLocalFile();
    await api.sendAcceleratedFile("peer-1", "local-file-1");
    await api.listMessageHistory("conv-1", "cursor/1");

    expect(fetchFn.mock.calls).toEqual([
      [
        "http://127.0.0.1:52350/api/bootstrap",
        expect.objectContaining({
          headers: expect.objectContaining({
            "Content-Type": "application/json",
          }),
        }),
      ],
      [
        "http://127.0.0.1:52350/api/pairings",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ peerDeviceId: "peer-1" }),
        }),
      ],
      [
        "http://127.0.0.1:52350/api/pairings/pair-1/confirm",
        expect.objectContaining({
          method: "POST",
        }),
      ],
      [
        "http://127.0.0.1:52350/api/peers/peer-1/messages/text",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ body: "hello" }),
        }),
      ],
      [
        "http://127.0.0.1:52350/api/local-files/pick",
        expect.objectContaining({
          method: "POST",
        }),
      ],
      [
        "http://127.0.0.1:52350/api/peers/peer-1/transfers/accelerated",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ localFileId: "local-file-1" }),
        }),
      ],
      [
        "http://127.0.0.1:52350/api/conversations/conv-1/messages?beforeCursor=cursor%2F1",
        expect.objectContaining({
          headers: expect.objectContaining({
            "Content-Type": "application/json",
          }),
        }),
      ],
    ]);
  });

  it("普通文件发送直接走浏览器选文件加 multipart 上传", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ transferId: "tx-1", fileName: "demo.txt" }),
      text: async () => "",
    });
    const pickedFile = new File(["hello"], "demo.txt", { type: "text/plain" });
    const pickFile = vi.fn().mockResolvedValue(pickedFile);
    const { createLocalhostApiClient } = await import("./localhost-api");
    const api = createLocalhostApiClient({
      origin: "http://127.0.0.1:52350",
      fetchFn,
      createEventSource: vi.fn(),
      pickFile,
    });

    await api.sendFile("peer-1");

    expect(pickFile).toHaveBeenCalledTimes(1);
    expect(fetchFn).toHaveBeenCalledWith(
      "http://127.0.0.1:52350/api/peers/peer-1/transfers/browser-upload",
      expect.objectContaining({
        method: "POST",
        body: expect.any(FormData),
      }),
    );

    const form = fetchFn.mock.calls[0]?.[1]?.body;
    expect(form).toBeInstanceOf(FormData);
    const uploadedFile = (form as FormData).get("file");
    expect(uploadedFile).toBeInstanceOf(File);
    expect((uploadedFile as File).name).toBe(pickedFile.name);
    expect((uploadedFile as File).size).toBe(pickedFile.size);
    expect((form as FormData).get("fileSize")).toBe(String(pickedFile.size));
    expect([...((form as FormData).keys())]).toEqual(["fileSize", "file"]);
  });

  it("普通文件发送支持直接传入拖拽文件并跳过浏览器选文件", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ transferId: "tx-1", fileName: "drop.txt" }),
      text: async () => "",
    });
    const droppedFile = new File(["drop-body"], "drop.txt", { type: "text/plain" });
    const pickFile = vi.fn();
    const { createLocalhostApiClient } = await import("./localhost-api");
    const api = createLocalhostApiClient({
      origin: "http://127.0.0.1:52350",
      fetchFn,
      createEventSource: vi.fn(),
      pickFile,
    });

    await api.sendFile("peer-1", droppedFile);

    expect(pickFile).not.toHaveBeenCalled();
    const form = fetchFn.mock.calls[0]?.[1]?.body;
    expect(form).toBeInstanceOf(FormData);
    const uploadedFile = (form as FormData).get("file");
    expect(uploadedFile).toBeInstanceOf(File);
    expect((uploadedFile as File).name).toBe(droppedFile.name);
    expect((uploadedFile as File).size).toBe(droppedFile.size);
    expect((form as FormData).get("fileSize")).toBe(String(droppedFile.size));
  });

  it("SSE 基于 eventSeq 去重，并在 reconnect 后从最新序号续接", async () => {
    const fetchFn = vi.fn();
    const sources: MockEventSource[] = [];
    const { createLocalhostApiClient } = await import("./localhost-api");
    const api = createLocalhostApiClient({
      origin: "http://127.0.0.1:52350",
      fetchFn,
      createEventSource: (url) => {
        const source = new MockEventSource(url);
        sources.push(source);
        return source as unknown as EventSource;
      },
      pickFile: vi.fn(),
    });

    const received: number[] = [];
    const subscription = api.subscribeEvents({
      lastEventSeq: 7,
      onEvent(event) {
        received.push(event.eventSeq);
      },
    });

    expect(sources).toHaveLength(1);
    expect(sources[0]?.url).toBe("http://127.0.0.1:52350/api/events/stream?afterSeq=7");

    sources[0]?.emit({ eventSeq: 8, kind: "peer.updated", payload: { deviceId: "peer-1" } });
    sources[0]?.emit({ eventSeq: 8, kind: "peer.updated", payload: { deviceId: "peer-1" } });
    sources[0]?.emit({ eventSeq: 9, kind: "health.updated", payload: { status: "ok" } });

    expect(received).toEqual([8, 9]);

    subscription.reconnect();

    expect(sources).toHaveLength(2);
    expect(sources[0]?.closed).toBe(true);
    expect(sources[1]?.url).toBe("http://127.0.0.1:52350/api/events/stream?afterSeq=9");

    sources[1]?.emit({ eventSeq: 9, kind: "health.updated", payload: { status: "stale" } });
    sources[1]?.emit({ eventSeq: 10, kind: "transfer.updated", payload: { transferId: "tx-1" } });

    expect(received).toEqual([8, 9, 10]);

    subscription.close();

    expect(sources[1]?.closed).toBe(true);
  });
});
