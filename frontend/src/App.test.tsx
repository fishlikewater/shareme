import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import App from "./App";
import type { LocalApi } from "./lib/api";
import type {
  AgentEvent,
  BootstrapSnapshot,
  MessageSnapshot,
  PairingSnapshot,
  TransferSnapshot,
} from "./lib/types";

class FakeApi implements LocalApi {
  private eventHandler?: (event: AgentEvent) => void;
  readonly startedPairings: string[] = [];
  readonly confirmedPairings: string[] = [];
  readonly sentTexts: Array<{ peerDeviceId: string; body: string }> = [];
  readonly sentFiles: Array<{ peerDeviceId: string; file: File }> = [];

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

  async sendFile(peerDeviceId: string, file: File): Promise<TransferSnapshot> {
    this.sentFiles.push({ peerDeviceId, file });
    return {
      transferId: "transfer-1",
      messageId: "msg-file-1",
      fileName: file.name,
      fileSize: file.size,
      state: "done",
      createdAt: "2026-04-10T08:25:00Z",
      direction: "outgoing",
      bytesTransferred: file.size,
      progressPercent: 100,
      rateBytesPerSec: 0,
      etaSeconds: 0,
      active: false,
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

describe("App", () => {
  it("渲染新的传输工作台并展示设备与历史消息", async () => {
    const api = new FakeApi(bootstrapSnapshot);

    render(<App api={api} />);

    expect(screen.getByText("正在连接本机代理")).toBeInTheDocument();
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

    const file = new File(["hello"], "hello.txt", { type: "text/plain" });
    expect(screen.getByRole("button", { name: "选择文件" })).toBeInTheDocument();
    fireEvent.change(screen.getByTestId("file-input"), {
      target: { files: [file] },
    });

    await waitFor(() => {
      expect(api.sentFiles).toHaveLength(1);
    });
    await waitFor(() => {
      expect(screen.getAllByText("hello.txt").length).toBeGreaterThan(0);
    });
  });

  it("bootstrap 失败时显示明确的连接错误页", async () => {
    const api: LocalApi = {
      bootstrap: vi.fn().mockRejectedValue(new Error("ECONNREFUSED")),
      startPairing: vi.fn(),
      confirmPairing: vi.fn(),
      sendText: vi.fn(),
      sendFile: vi.fn(),
      subscribeEvents: vi.fn(() => ({
        close: vi.fn(),
        reconnect: vi.fn(),
      })),
    };

    render(<App api={api} />);

    expect(await screen.findByText("无法连接本机代理")).toBeInTheDocument();
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
    expect(within(banner).getByText("ETA 00:04")).toBeInTheDocument();
  });
});
