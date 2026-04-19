import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import App from "./App";
import type { LocalApi } from "./lib/api";
import type {
  AgentEvent,
  BootstrapSnapshot,
  LocalFileSnapshot,
  MessageHistoryPage,
  MessageSnapshot,
  PairingSnapshot,
  TransferSnapshot,
} from "./lib/types";

afterEach(() => {
  vi.useRealTimers();
});

class FakeApi implements LocalApi {
  private eventHandler?: (event: AgentEvent) => void;
  readonly startedPairings: string[] = [];
  readonly confirmedPairings: string[] = [];
  readonly sentTexts: Array<{ peerDeviceId: string; body: string }> = [];
  readonly sentFiles: Array<{ peerDeviceId: string }> = [];
  readonly pickedLocalFiles: number[] = [];
  readonly sentAcceleratedFiles: Array<{ peerDeviceId: string; localFileId: string }> = [];
  readonly listedHistory: Array<{ conversationId: string; beforeCursor?: string }> = [];

  constructor(private readonly snapshot: BootstrapSnapshot) {}

  async bootstrap(): Promise<BootstrapSnapshot> {
    return this.snapshot;
  }

  async startPairing(peerDeviceId: string): Promise<PairingSnapshot> {
    this.startedPairings.push(peerDeviceId);
    return {
      pairingId: "pair-2",
      peerDeviceId,
      peerDeviceName: "会议室电脑",
      shortCode: "314159",
      status: "pending",
    };
  }

  async confirmPairing(pairingId: string): Promise<PairingSnapshot> {
    this.confirmedPairings.push(pairingId);
    return {
      pairingId,
      peerDeviceId: "peer-2",
      peerDeviceName: "会议室电脑",
      shortCode: "314159",
      status: "confirmed",
    };
  }

  async sendText(peerDeviceId: string, body: string): Promise<MessageSnapshot> {
    this.sentTexts.push({ peerDeviceId, body });
    return {
      messageId: "msg-out-1",
      conversationId: `conv-${peerDeviceId}`,
      direction: "outgoing",
      kind: "text",
      body,
      status: "sent",
      createdAt: "2026-04-10T08:20:00Z",
    };
  }

  async sendFile(peerDeviceId: string): Promise<TransferSnapshot> {
    this.sentFiles.push({ peerDeviceId });
    return {
      transferId: "transfer-1",
      messageId: "msg-file-1",
      fileName: "hello.txt",
      fileSize: 5,
      state: "done",
      createdAt: "2026-04-10T08:25:00Z",
      direction: "outgoing",
      bytesTransferred: 5,
      progressPercent: 100,
      rateBytesPerSec: 0,
      etaSeconds: 0,
      active: false,
    };
  }

  async pickLocalFile(): Promise<LocalFileSnapshot> {
    this.pickedLocalFiles.push(Date.now());
    return {
      localFileId: "lf-1",
      displayName: "archive.iso",
      size: 3_221_225_472,
      acceleratedEligible: true,
    };
  }

  async sendAcceleratedFile(peerDeviceId: string, localFileId: string): Promise<TransferSnapshot> {
    this.sentAcceleratedFiles.push({ peerDeviceId, localFileId });
    return {
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
    };
  }

  async listMessageHistory(conversationId: string, beforeCursor?: string): Promise<MessageHistoryPage> {
    this.listedHistory.push({ conversationId, beforeCursor });
    return {
      conversationId,
      messages: [],
      hasMore: false,
      nextCursor: "",
    };
  }

  subscribeEvents(options: { lastEventSeq?: number; onEvent: (event: AgentEvent) => void }) {
    this.eventHandler = options.onEvent;
    return {
      close: vi.fn(),
      reconnect: vi.fn(),
    };
  }

  emit(event: AgentEvent) {
    this.eventHandler?.(event);
  }
}

const bootstrapSnapshot: BootstrapSnapshot = {
  localDeviceName: "我的电脑",
  health: {
    status: "degraded",
    discovery: "broadcast-ok",
    localAPIReady: true,
    agentPort: 19090,
    issues: ["自动发现正常，但有一台设备暂时不可达"],
  },
  peers: [
    {
      deviceId: "peer-1",
      deviceName: "办公室副机",
      trusted: true,
      online: true,
      reachable: true,
      agentTcpPort: 19090,
    },
    {
      deviceId: "peer-2",
      deviceName: "会议室电脑",
      trusted: false,
      online: true,
      reachable: true,
      agentTcpPort: 19090,
    },
    {
      deviceId: "peer-3",
      deviceName: "测试笔记本",
      trusted: true,
      online: true,
      reachable: false,
      agentTcpPort: 19090,
    },
  ],
  pairings: [],
  conversations: [
    {
      conversationId: "conv-peer-1",
      peerDeviceId: "peer-1",
      peerDeviceName: "办公室副机",
    },
  ],
  messages: [
    {
      messageId: "msg-in-1",
      conversationId: "conv-peer-1",
      direction: "incoming",
      kind: "text",
      body: "昨晚的文件已经收到",
      status: "sent",
      createdAt: "2026-04-10T08:00:00Z",
    },
  ],
  transfers: [],
  eventSeq: 7,
};

const activeTransferSnapshot: TransferSnapshot = {
  transferId: "transfer-banner-1",
  messageId: "msg-banner-1",
  fileName: "demo.pdf",
  fileSize: 2_097_152,
  state: "sending",
  createdAt: "2026-04-10T09:00:00Z",
  direction: "outgoing",
  bytesTransferred: 1_048_576,
  progressPercent: 50,
  rateBytesPerSec: 512_000,
  etaSeconds: 4,
  active: true,
};

const bootstrapSnapshotWithActiveTransfer: BootstrapSnapshot = {
  ...bootstrapSnapshot,
  messages: [
    ...bootstrapSnapshot.messages,
    {
      messageId: "msg-banner-1",
      conversationId: "conv-peer-1",
      direction: "outgoing",
      kind: "file",
      body: "demo.pdf",
      status: "sending",
      createdAt: "2026-04-10T09:00:00Z",
    },
  ],
  transfers: [activeTransferSnapshot],
};

function buildSnapshotWithHistory(total: number): BootstrapSnapshot {
  return {
    ...bootstrapSnapshot,
    conversations: [
      {
        conversationId: "conv-peer-1",
        peerDeviceId: "peer-1",
        peerDeviceName: "鍔炲叕瀹ゅ壇鏈?",
        hasMoreHistory: total > 10,
        nextCursor: total > 10 ? "cursor-peer-1" : "",
      },
    ],
    messages: Array.from({ length: total }, (_, index) => ({
      messageId: `msg-${index.toString().padStart(2, "0")}`,
      conversationId: "conv-peer-1",
      direction: "incoming" as const,
      kind: "text" as const,
      body: `body-${index.toString().padStart(2, "0")}`,
      status: "sent" as const,
      createdAt: `2026-04-17T10:00:${index.toString().padStart(2, "0")}Z`,
    })).slice(-10),
  };
}

describe("App", () => {
  it("渲染新的传输工作台并展示设备与历史消息", async () => {
    const api = new FakeApi(bootstrapSnapshot);

    render(<App api={api} />);

    expect(screen.getByText("正在启动桌面运行时")).toBeInTheDocument();
    expect((await screen.findAllByText("我的电脑")).length).toBeGreaterThan(0);
    expect(screen.getByText("已发现 3 台设备")).toBeInTheDocument();
    expect(screen.getByText("文字与文件都会直连传输，不经过云端。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /办公室副机/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: /会议室电脑/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /测试笔记本/ })).toBeInTheDocument();
    expect(screen.getByText("自动发现正常，但有一台设备暂时不可达")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /办公室副机/ }));

    expect(screen.getByText("实时沟通")).toBeInTheDocument();
    expect(screen.getAllByText("昨晚的文件已经收到").length).toBeGreaterThan(0);
    expect(screen.getByRole("textbox", { name: "消息输入框" })).toBeInTheDocument();
  });

  it("未配对设备会突出显示配对流程，确认后进入发送状态", async () => {
    const api = new FakeApi(bootstrapSnapshot);

    render(<App api={api} />);
    expect((await screen.findAllByText("我的电脑")).length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: /会议室电脑/ }));
    expect(screen.getByText("建立信任后再发送")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "开始配对" }));

    await waitFor(() => {
      expect(api.startedPairings).toEqual(["peer-2"]);
    });
    expect(await screen.findByText("314159")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "确认配对" }));
    await waitFor(() => {
      expect(api.confirmedPairings).toEqual(["pair-2"]);
    });

    expect(await screen.findByText("314159")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "开始配对" })).not.toBeInTheDocument();
    expect(screen.getByText("已确认短码，正在等待设备完成信任同步。")).toBeInTheDocument();

    act(() => {
      api.emit({
        eventSeq: 9,
        kind: "peer.updated",
        payload: {
          deviceId: "peer-2",
          deviceName: "会议室电脑",
          trusted: true,
          online: true,
          reachable: true,
        },
      });
    });

    expect(screen.getAllByText("可以开始发送文字、图片以外的任意文件").length).toBeGreaterThan(0);
    expect(screen.getByRole("textbox", { name: "消息输入框" })).toBeInTheDocument();
  });

  it("可以发送文字和文件，并展示到当前会话", async () => {
    const api = new FakeApi(bootstrapSnapshot);

    render(<App api={api} />);
    expect((await screen.findAllByText("我的电脑")).length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: /办公室副机/ }));

    fireEvent.change(screen.getByRole("textbox", { name: "消息输入框" }), {
      target: { value: "你好" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送文字" }));

    await waitFor(() => {
      expect(api.sentTexts).toEqual([{ peerDeviceId: "peer-1", body: "你好" }]);
    });
    await waitFor(() => {
      expect(screen.getAllByText("你好").length).toBeGreaterThan(0);
    });

    expect(screen.getByRole("button", { name: "选择文件" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "选择文件" }));

    await waitFor(() => {
      expect(api.sentFiles).toHaveLength(1);
    });
    await waitFor(() => {
      expect(screen.getAllByText("hello.txt").length).toBeGreaterThan(0);
    });
  });

  it("可以选择本地大文件并发起极速发送", async () => {
    const api = new FakeApi(bootstrapSnapshot);

    render(<App api={api} />);
    expect((await screen.findAllByText("我的电脑")).length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: /办公室副机/ }));

    fireEvent.click(screen.getByRole("button", { name: "极速发送大文件" }));

    await waitFor(() => {
      expect(api.pickedLocalFiles).toHaveLength(1);
    });

    expect(screen.getByText("已选本地文件")).toBeInTheDocument();
    expect(screen.getByText("archive.iso")).toBeInTheDocument();
    expect(screen.getByText("满足极速条件")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "发送已选大文件" }));

    await waitFor(() => {
      expect(api.sentAcceleratedFiles).toEqual([
        { peerDeviceId: "peer-1", localFileId: "lf-1" },
      ]);
    });
    await waitFor(() => {
      expect(screen.getAllByText("archive.iso").length).toBeGreaterThan(0);
    });
  });

  it("桌面宿主选中的非极速文件也可以继续走普通文件发送闭环", async () => {
    const api = new FakeApi(bootstrapSnapshot);
    vi.spyOn(api, "pickLocalFile").mockResolvedValue({
      localFileId: "lf-small",
      displayName: "notes.txt",
      size: 1024,
      acceleratedEligible: false,
    });

    render(<App api={api} />);
    expect((await screen.findAllByText("我的电脑")).length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: /办公室副机/ }));
    fireEvent.click(screen.getByRole("button", { name: "极速发送大文件" }));

    expect(await screen.findByText("已选本地文件")).toBeInTheDocument();
    expect(screen.getByText("当前文件会继续走普通文件传输")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "发送已选文件" }));

    await waitFor(() => {
      expect(api.sentAcceleratedFiles).toEqual([
        { peerDeviceId: "peer-1", localFileId: "lf-small" },
      ]);
    });
  });

  it("bootstrap 失败时显示明确的连接错误页", async () => {
    const api: LocalApi = {
      bootstrap: vi.fn().mockRejectedValue(new Error("ECONNREFUSED")),
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

    expect(await screen.findByText("无法启动桌面运行时")).toBeInTheDocument();
    expect(screen.getByText("ECONNREFUSED")).toBeInTheDocument();
  });

  it("顶部横幅会展示处于活动状态的文件传输", async () => {
    const api = new FakeApi(bootstrapSnapshotWithActiveTransfer);

    render(<App api={api} />);

    const banner = await screen.findByLabelText("传输横幅");
    expect(within(banner).getByText("文件传输")).toBeInTheDocument();
    expect(within(banner).getByText("demo.pdf")).toBeInTheDocument();
    expect(within(banner).getByText("50%")).toBeInTheDocument();
    expect(within(banner).getByText("发送")).toBeInTheDocument();
    expect(within(banner).getByText("预计 4 秒")).toBeInTheDocument();
  });

  it("极速传输回退事件会更新现有记录并提示正在回退普通传输", async () => {
    const api = new FakeApi(bootstrapSnapshotWithActiveTransfer);

    render(<App api={api} />);

    expect(await screen.findByLabelText("传输横幅")).toBeInTheDocument();
    expect(screen.getAllByText("demo.pdf")).toHaveLength(2);

    act(() => {
      api.emit({
        eventSeq: 10,
        kind: "transfer.updated",
        payload: {
          ...activeTransferSnapshot,
          state: "fallback_pending",
          progressPercent: 50,
          rateBytesPerSec: 0,
          etaSeconds: null,
        },
      });
    });

    expect(screen.getAllByText("准备回退普通传输").length).toBeGreaterThan(0);
    expect(screen.getAllByText("demo.pdf")).toHaveLength(2);
  });

  it("only renders the newest ten bootstrap messages for the active conversation", async () => {
    const api = new FakeApi(buildSnapshotWithHistory(12));

    render(<App api={api} />);

    const list = await screen.findByTestId("message-list");
    expect(within(list).getByText("body-11")).toBeInTheDocument();
    expect(within(list).queryByText("body-00")).not.toBeInTheDocument();
  });

  it("loads older messages when scrolling near the top", async () => {
    const api = new FakeApi(buildSnapshotWithHistory(12));
    vi.spyOn(api, "listMessageHistory").mockResolvedValue({
      conversationId: "conv-peer-1",
      hasMore: false,
      nextCursor: "",
      messages: [
        {
          messageId: "msg-00",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "body-00",
          status: "sent",
          createdAt: "2026-04-17T10:00:00Z",
        },
        {
          messageId: "msg-01",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "body-01",
          status: "sent",
          createdAt: "2026-04-17T10:00:01Z",
        },
      ],
    });

    render(<App api={api} />);

    const list = await screen.findByTestId("message-list");
    fireEvent.scroll(list, { target: { scrollTop: 0 } });

    await waitFor(() => {
      expect(api.listMessageHistory).toHaveBeenCalledWith("conv-peer-1", "cursor-peer-1");
    });
    expect(await screen.findByText("body-00")).toBeInTheDocument();
  });

  it("keeps loaded history when a realtime message arrives", async () => {
    const api = new FakeApi(buildSnapshotWithHistory(12));
    vi.spyOn(api, "listMessageHistory").mockResolvedValue({
      conversationId: "conv-peer-1",
      hasMore: false,
      nextCursor: "",
      messages: [
        {
          messageId: "msg-00",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "body-00",
          status: "sent",
          createdAt: "2026-04-17T10:00:00Z",
        },
        {
          messageId: "msg-01",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "body-01",
          status: "sent",
          createdAt: "2026-04-17T10:00:01Z",
        },
      ],
    });

    render(<App api={api} />);

    const list = await screen.findByTestId("message-list");
    fireEvent.scroll(list, { target: { scrollTop: 0 } });
    expect(await screen.findByText("body-00")).toBeInTheDocument();

    act(() => {
      api.emit({
        eventSeq: 99,
        kind: "message.upserted",
        payload: {
          messageId: "msg-live",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "live-body",
          status: "sent",
          createdAt: "2026-04-17T10:01:00Z",
        },
      });
    });

    expect(within(list).getByText("live-body")).toBeInTheDocument();
    expect(within(list).getAllByText("body-00")).toHaveLength(1);
  });

  it("does not duplicate messages when paged history overlaps with realtime updates", async () => {
    const api = new FakeApi(buildSnapshotWithHistory(12));
    vi.spyOn(api, "listMessageHistory").mockResolvedValue({
      conversationId: "conv-peer-1",
      hasMore: false,
      nextCursor: "",
      messages: [
        {
          messageId: "msg-09",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "body-09",
          status: "sent",
          createdAt: "2026-04-17T10:00:09Z",
        },
        {
          messageId: "msg-00",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "body-00",
          status: "sent",
          createdAt: "2026-04-17T10:00:00Z",
        },
      ],
    });

    render(<App api={api} />);

    const list = await screen.findByTestId("message-list");
    fireEvent.scroll(list, { target: { scrollTop: 0 } });

    await waitFor(() => {
      expect(api.listMessageHistory).toHaveBeenCalledWith("conv-peer-1", "cursor-peer-1");
    });

    act(() => {
      api.emit({
        eventSeq: 100,
        kind: "message.upserted",
        payload: {
          messageId: "msg-00",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "body-00",
          status: "sent",
          createdAt: "2026-04-17T10:00:00Z",
        },
      });
    });

    expect(within(list).getAllByText("body-00")).toHaveLength(1);
    expect(within(list).getAllByText("body-09")).toHaveLength(1);
  });

  it("preserves loaded history after periodic bootstrap refresh", async () => {
    vi.useFakeTimers();

    const api = new FakeApi(buildSnapshotWithHistory(12));
    const bootstrapSpy = vi.spyOn(api, "bootstrap");
    vi.spyOn(api, "listMessageHistory").mockResolvedValue({
      conversationId: "conv-peer-1",
      hasMore: false,
      nextCursor: "",
      messages: [
        {
          messageId: "msg-00",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "body-00",
          status: "sent",
          createdAt: "2026-04-17T10:00:00Z",
        },
        {
          messageId: "msg-01",
          conversationId: "conv-peer-1",
          direction: "incoming",
          kind: "text",
          body: "body-01",
          status: "sent",
          createdAt: "2026-04-17T10:00:01Z",
        },
      ],
    });

    render(<App api={api} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    const list = screen.getByTestId("message-list");
    fireEvent.scroll(list, { target: { scrollTop: 0 } });
    await act(async () => {
      await Promise.resolve();
    });
    expect(within(list).getByText("body-00")).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });

    expect(bootstrapSpy).toHaveBeenCalledTimes(2);
    expect(within(list).getByText("body-00")).toBeInTheDocument();
  });
});
