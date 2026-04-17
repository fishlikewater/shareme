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

  close() {
    this.onclose?.();
  }

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

  it("调用本机 agent 选择本地文件并返回安全引用", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          localFileId: "lf-1",
          displayName: "archive.iso",
          size: 3_221_225_472,
          acceleratedEligible: true,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );

    const api = createLocalApiClient({ fetchImpl: fetchMock });
    const localFile = await api.pickLocalFile();

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(localFile.localFileId).toBe("lf-1");
    expect(url).toBe(`${window.location.origin}/api/local-files/pick`);
    expect(init.method).toBe("POST");
  });

  it("发起极速发送时使用 localFileId 而不是浏览器文件对象", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          transferId: "transfer-accelerated-1",
          messageId: "msg-accelerated-1",
          fileName: "archive.iso",
          fileSize: 3_221_225_472,
          state: "preparing",
          createdAt: "2026-04-16T09:00:00Z",
          direction: "outgoing",
          bytesTransferred: 0,
          progressPercent: 0,
          rateBytesPerSec: 0,
          etaSeconds: null,
          active: true,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );

    const api = createLocalApiClient({ fetchImpl: fetchMock });
    await api.sendAcceleratedFile("peer-1", "lf-1");

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(`${window.location.origin}/api/transfers/accelerated`);
    expect(init.method).toBe("POST");
    expect(JSON.parse(String(init.body))).toEqual({
      peerDeviceId: "peer-1",
      localFileId: "lf-1",
    });
  });

  it("极速发送失败时透传后端错误正文", async () => {
    const api = createLocalApiClient({
      fetchImpl: vi.fn().mockResolvedValue(new Response("本地文件已失效，请重新选择", { status: 410 })),
    });

    await expect(api.sendAcceleratedFile("peer-1", "lf-1")).rejects.toThrow("本地文件已失效，请重新选择");
  });

  it("本地文件选择失败时透传后端错误正文", async () => {
    const api = createLocalApiClient({
      fetchImpl: vi.fn().mockResolvedValue(new Response("origin not allowed", { status: 403 })),
    });

    await expect(api.pickLocalFile()).rejects.toThrow("origin not allowed");
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

  it("手动 reconnect 不会在延迟任务里再额外补开第三个 socket", async () => {
    vi.useFakeTimers();

    const api = createLocalApiClient({
      webSocketFactory: (url) => new FakeWebSocket(url),
    });

    const stream = api.subscribeEvents({
      lastEventSeq: 2,
      onEvent() {},
    });

    expect(FakeWebSocket.instances).toHaveLength(1);
    stream.reconnect();
    expect(FakeWebSocket.instances).toHaveLength(2);

    await vi.advanceTimersByTimeAsync(1100);

    expect(FakeWebSocket.instances).toHaveLength(2);
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

  it("loads older message history for a conversation", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          conversationId: "conv-peer-1",
          messages: [
            {
              messageId: "msg-01",
              conversationId: "conv-peer-1",
              direction: "incoming",
              kind: "text",
              body: "older body",
              status: "sent",
              createdAt: "2026-04-17T10:00:01Z",
            },
          ],
          hasMore: true,
          nextCursor: "cursor-2",
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );

    const api = createLocalApiClient({ fetchImpl: fetchMock });
    const page = await api.listMessageHistory("conv-peer-1", "cursor-1");

    expect(page.conversationId).toBe("conv-peer-1");
    expect(page.nextCursor).toBe("cursor-2");
    expect(fetchMock).toHaveBeenCalledWith(
      `${window.location.origin}/api/conversations/conv-peer-1/messages?before=cursor-1`,
      expect.objectContaining({ method: "GET" }),
    );
  });
});
