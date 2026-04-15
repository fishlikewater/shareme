import { afterEach, describe, expect, it, vi } from "vitest";

import { createLocalApiClient } from "./api";
import type { AgentEvent } from "./types";

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];

  readonly url: string;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  close() {}

  emit(event: AgentEvent) {
    this.onmessage?.({
      data: JSON.stringify(event),
    } as MessageEvent<string>);
  }

  emitClose() {
    this.onclose?.();
  }

  emitError() {
    this.onerror?.();
  }
}

afterEach(() => {
  FakeWebSocket.instances = [];
  window.history.pushState({}, "", "/");
  vi.useRealTimers();
});

describe("createLocalApiClient", () => {
  it("调用 bootstrap 接口拉取当前快照", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          localDeviceName: "我的电脑",
          health: { status: "ok", discovery: "broadcast-ok", localAPIReady: true },
          peers: [],
          pairings: [],
          conversations: [],
          messages: [],
          transfers: [],
          eventSeq: 3,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );

    const api = createLocalApiClient({ fetchImpl: fetchMock });
    const snapshot = await api.bootstrap();

    expect(snapshot.localDeviceName).toBe("我的电脑");
    expect(fetchMock).toHaveBeenCalledWith(`${window.location.origin}/api/bootstrap`, expect.objectContaining({ method: "GET" }));
  });

  it("发起配对时调用真实后端路由", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ pairingId: "pair-1" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const api = createLocalApiClient({ fetchImpl: fetchMock });
    await api.startPairing("peer-1");

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(`${window.location.origin}/api/pairings`);
    expect(init.method).toBe("POST");
    expect(JSON.parse(String(init.body))).toEqual({ peerDeviceId: "peer-1" });
  });

  it("发送文件时使用 multipart/form-data", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ transferId: "transfer-1", messageId: "msg-1" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const api = createLocalApiClient({ fetchImpl: fetchMock });
    const file = new File(["hello"], "hello.txt", { type: "text/plain" });

    await api.sendFile("peer-1", file);

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(`${window.location.origin}/api/transfers/file`);
    expect(init.method).toBe("POST");
    expect(init.body).toBeInstanceOf(FormData);

    const body = init.body as FormData;
    expect(body.get("peerDeviceId")).toBe("peer-1");
    expect(body.get("fileSize")).toBe(String(file.size));
    expect((body.get("file") as File).name).toBe("hello.txt");
    expect(Array.from(body.keys())).toEqual(["peerDeviceId", "fileSize", "file"]);
  });

  it("事件流手动重连时会带上最近处理成功的 eventSeq", () => {
    const events: AgentEvent[] = [];
    const api = createLocalApiClient({
      webSocketFactory: (url) => new FakeWebSocket(url),
    });

    const stream = api.subscribeEvents({
      lastEventSeq: 3,
      onEvent(event) {
        events.push(event);
      },
    });

    expect(FakeWebSocket.instances[0]?.url).toContain("lastEventSeq=3");

    FakeWebSocket.instances[0]?.emit({
      eventSeq: 8,
      kind: "peer.updated",
      payload: { deviceId: "peer-1", trusted: true },
    });

    stream.reconnect();

    expect(events[0]?.eventSeq).toBe(8);
    expect(FakeWebSocket.instances[1]?.url).toContain("lastEventSeq=8");
    stream.close();
  });

  it("支持从运行时 query 参数读取本地 API 地址", async () => {
    window.history.pushState({}, "", "/?localApi=http://127.0.0.1:19101");
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          localDeviceName: "我的电脑",
          health: { status: "ok", discovery: "broadcast-ok", localAPIReady: true },
          peers: [],
          pairings: [],
          conversations: [],
          messages: [],
          transfers: [],
          eventSeq: 1,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );

    const api = createLocalApiClient({
      fetchImpl: fetchMock,
      webSocketFactory: (url) => new FakeWebSocket(url),
    });

    await api.bootstrap();
    const stream = api.subscribeEvents({
      lastEventSeq: 1,
      onEvent() {},
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "http://127.0.0.1:19101/api/bootstrap",
      expect.objectContaining({ method: "GET" }),
    );
    expect(FakeWebSocket.instances[0]?.url).toContain("ws://127.0.0.1:19101/api/events/stream");
    stream.close();
  });

  it("嵌入式页面场景下默认使用当前页面同源的本地 API", async () => {
    window.history.pushState({}, "", "/embedded");
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          localDeviceName: "我的电脑",
          health: { status: "ok", discovery: "broadcast-ok", localAPIReady: true },
          peers: [],
          pairings: [],
          conversations: [],
          messages: [],
          transfers: [],
          eventSeq: 1,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );

    const api = createLocalApiClient({ fetchImpl: fetchMock });
    await api.bootstrap();

    expect(fetchMock).toHaveBeenCalledWith(
      `${window.location.origin}/api/bootstrap`,
      expect.objectContaining({ method: "GET" }),
    );
  });

  it("websocket 异常关闭后会带上最新 eventSeq 自动重连", async () => {
    vi.useFakeTimers();

    const api = createLocalApiClient({
      fetchImpl: vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ events: [], lastEventSeq: 8 }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
      webSocketFactory: (url) => new FakeWebSocket(url),
    });

    const stream = api.subscribeEvents({
      lastEventSeq: 3,
      onEvent() {},
    });

    FakeWebSocket.instances[0]?.emit({
      eventSeq: 8,
      kind: "peer.updated",
      payload: { deviceId: "peer-1", trusted: true },
    });
    FakeWebSocket.instances[0]?.emitClose();

    await vi.advanceTimersByTimeAsync(1000);

    expect(FakeWebSocket.instances[1]?.url).toContain("lastEventSeq=8");
    stream.close();
  });

  it("会定时回放遗漏的事件，避免界面静默陈旧", async () => {
    vi.useFakeTimers();

    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          events: [
            {
              eventSeq: 5,
              kind: "peer.updated",
              payload: { deviceId: "peer-1", trusted: true },
            },
          ],
          lastEventSeq: 5,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );

    const events: AgentEvent[] = [];
    const api = createLocalApiClient({
      fetchImpl: fetchMock,
      webSocketFactory: (url) => new FakeWebSocket(url),
    });

    const stream = api.subscribeEvents({
      lastEventSeq: 3,
      onEvent(event) {
        events.push(event);
      },
    });

    await vi.advanceTimersByTimeAsync(1500);

    expect(fetchMock).toHaveBeenCalledWith(`${window.location.origin}/api/events?afterSeq=3`, expect.objectContaining({ method: "GET" }));
    expect(events.map((event) => event.eventSeq)).toEqual([5]);
    stream.close();
  });
});
