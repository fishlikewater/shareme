import { startTransition, useEffect, useMemo, useRef, useState } from "react";

import { ChatPane } from "./components/ChatPane";
import { PairCodeDialog } from "./components/PairCodeDialog";
import { WorkbenchStatusPanel } from "./components/WorkbenchStatusPanel";
import { createDefaultLocalApi, type LocalApi } from "./lib/api";
import { notifyDesktopUiReady } from "./lib/desktop-api";
import type {
  AgentEvent,
  BootstrapSnapshot,
  ConversationMessage,
  ConversationSnapshot,
  LocalFileSnapshot,
  MessageSnapshot,
  PairingSnapshot,
  PeerSnapshot,
  PeerSummary,
  TransferSnapshot,
} from "./lib/types";
import { DiscoveryPage } from "./pages/DiscoveryPage";

type AppProps = {
  api?: LocalApi;
};

type BusyState = {
  startingPairing: boolean;
  confirmingPairing: boolean;
  sendingText: boolean;
  sendingFile: boolean;
  pickingLocalFile: boolean;
  sendingAcceleratedFile: boolean;
};

type ConversationHistoryState = {
  olderMessages: MessageSnapshot[];
  hasMore: boolean;
  nextCursor?: string;
  loading: boolean;
  error?: string;
};

type WailsDropRuntime = {
  OnFileDrop?: (callback: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean) => void;
  OnFileDropOff?: () => void;
};

const initialBusyState: BusyState = {
  startingPairing: false,
  confirmingPairing: false,
  sendingText: false,
  sendingFile: false,
  pickingLocalFile: false,
  sendingAcceleratedFile: false,
};

const SNAPSHOT_REFRESH_INTERVAL_MS = 3000;

export default function AppShell({ api }: AppProps) {
  const [defaultApi] = useState<LocalApi | undefined>(() => (api ? undefined : createDefaultLocalApi()));
  const resolvedApi = api ?? defaultApi!;
  const mainColumnRef = useRef<HTMLElement | null>(null);
  const uiReadyReportedRef = useRef(false);
  const [snapshot, setSnapshot] = useState<BootstrapSnapshot | null>(null);
  const [selectedPeerId, setSelectedPeerId] = useState<string>();
  const [errorMessage, setErrorMessage] = useState<string>();
  const [commandError, setCommandError] = useState<string>();
  const [busyState, setBusyState] = useState<BusyState>(initialBusyState);
  const [pickedLocalFile, setPickedLocalFile] = useState<LocalFileSnapshot | null>(null);
  const [deviceDockCollapsed, setDeviceDockCollapsed] = useState(false);
  const [historyStateByConversation, setHistoryStateByConversation] = useState<
    Record<string, ConversationHistoryState | undefined>
  >({});

  useEffect(() => {
    if (!snapshot || errorMessage || uiReadyReportedRef.current) {
      return;
    }
    uiReadyReportedRef.current = true;
    void notifyDesktopUiReady().catch(() => {
      uiReadyReportedRef.current = false;
    });
  }, [errorMessage, snapshot]);

  useEffect(() => {
    let subscription: ReturnType<LocalApi["subscribeEvents"]> | undefined;
    let disposed = false;
    let loading = false;

    const applySnapshot = (nextSnapshot: BootstrapSnapshot) => {
      setErrorMessage(undefined);
      setSnapshot((current) => reconcileBootstrapSnapshot(current, nextSnapshot));
    };

    const load = async (subscribeAfterLoad: boolean) => {
      if (loading) {
        return;
      }
      loading = true;
      try {
        const nextSnapshot = await resolvedApi.bootstrap();
        if (disposed) {
          return;
        }

        applySnapshot(nextSnapshot);

        if (!subscription) {
          subscription = resolvedApi.subscribeEvents({
            lastEventSeq: nextSnapshot.eventSeq ?? 0,
            onEvent: (event) => {
              startTransition(() => {
                setSnapshot((current) => applyEvent(current, event));
              });
            },
          });
        }
      } catch (error) {
        if (disposed || !subscribeAfterLoad) {
          return;
        }
        setErrorMessage(error instanceof Error ? error.message : "unknown error");
      } finally {
        loading = false;
      }
    };

    void load(true);
    const refreshTimer = window.setInterval(() => {
      void load(false);
    }, SNAPSHOT_REFRESH_INTERVAL_MS);

    return () => {
      disposed = true;
      window.clearInterval(refreshTimer);
      subscription?.close();
    };
  }, [resolvedApi]);

  useEffect(() => {
    if (!snapshot) {
      return;
    }
    setSelectedPeerId((current) =>
      current && snapshot.peers.some((peer) => peer.deviceId === current) ? current : pickDefaultPeerId(snapshot),
    );
  }, [snapshot]);

  const peers = useMemo(() => (snapshot ? buildPeerSummaries(snapshot) : []), [snapshot]);
  const selectedPeer = useMemo(
    () => peers.find((peer) => peer.deviceId === selectedPeerId),
    [peers, selectedPeerId],
  );
  const selectedConversation = useMemo(
    () =>
      snapshot && selectedPeer
        ? snapshot.conversations.find((conversation) => conversation.peerDeviceId === selectedPeer.deviceId)
        : undefined,
    [selectedPeer, snapshot],
  );
  const selectedHistoryState = useMemo(
    () => (selectedConversation ? historyStateByConversation[selectedConversation.conversationId] : undefined),
    [historyStateByConversation, selectedConversation],
  );
  const selectedMessages = useMemo(
    () =>
      snapshot && selectedPeer
        ? buildConversationMessages(snapshot, selectedPeer.deviceId, selectedHistoryState?.olderMessages ?? [])
        : [],
    [selectedHistoryState?.olderMessages, selectedPeer, snapshot],
  );
  const selectedHistoryHasMore = selectedHistoryState?.hasMore ?? Boolean(selectedConversation?.hasMoreHistory);
  const selectedHistoryLoading = selectedHistoryState?.loading ?? false;
  const selectedHistoryError = selectedHistoryState?.error;
  const activeTransfers = useMemo(
    () => (snapshot ? snapshot.transfers.filter((transfer) => transfer.active) : []),
    [snapshot],
  );

  async function handleStartPairing() {
    if (!selectedPeer) {
      return;
    }

    setCommandError(undefined);
    setBusyState((current) => ({ ...current, startingPairing: true }));
    try {
      const pairing = await resolvedApi.startPairing(selectedPeer.deviceId);
      startTransition(() => {
        setSnapshot((current) => (current ? upsertPairing(current, pairing) : current));
      });
    } catch (error) {
      setCommandError(error instanceof Error ? error.message : "start pairing failed");
    } finally {
      setBusyState((current) => ({ ...current, startingPairing: false }));
    }
  }

  async function handleConfirmPairing(pairingId: string) {
    setCommandError(undefined);
    setBusyState((current) => ({ ...current, confirmingPairing: true }));
    try {
      const pairing = await resolvedApi.confirmPairing(pairingId);
      startTransition(() => {
        setSnapshot((current) => (current ? upsertPairing(current, pairing) : current));
      });
    } catch (error) {
      setCommandError(error instanceof Error ? error.message : "confirm pairing failed");
    } finally {
      setBusyState((current) => ({ ...current, confirmingPairing: false }));
    }
  }

  async function handleSendText(body: string) {
    if (!selectedPeer) {
      return;
    }

    setCommandError(undefined);
    setBusyState((current) => ({ ...current, sendingText: true }));
    try {
      const message = await resolvedApi.sendText(selectedPeer.deviceId, body);
      startTransition(() => {
        setSnapshot((current) =>
          current ? upsertMessageForPeer(current, selectedPeer.deviceId, message) : current,
        );
      });
    } catch (error) {
      setCommandError(error instanceof Error ? error.message : "send text failed");
    } finally {
      setBusyState((current) => ({ ...current, sendingText: false }));
    }
  }

  async function handleSendFile(file?: File) {
    if (!selectedPeer) {
      return;
    }

    setCommandError(undefined);
    setBusyState((current) => ({ ...current, sendingFile: true }));
    try {
      const transfer = await resolvedApi.sendFile(selectedPeer.deviceId, file);
      startTransition(() => {
        setSnapshot((current) =>
          current ? upsertOutgoingFile(current, selectedPeer.deviceId, transfer) : current,
        );
      });
    } catch (error) {
      setCommandError(error instanceof Error ? error.message : "send file failed");
    } finally {
      setBusyState((current) => ({ ...current, sendingFile: false }));
    }
  }

  async function handleSendFilePath(path: string) {
    if (
      !selectedPeer ||
      !resolvedApi.sendFilePath ||
      busyState.sendingFile ||
      busyState.pickingLocalFile ||
      busyState.sendingAcceleratedFile
    ) {
      return;
    }

    setCommandError(undefined);
    setBusyState((current) => ({ ...current, sendingFile: true }));
    try {
      const transfer = await resolvedApi.sendFilePath(selectedPeer.deviceId, path);
      startTransition(() => {
        setSnapshot((current) =>
          current ? upsertOutgoingFile(current, selectedPeer.deviceId, transfer) : current,
        );
      });
    } catch (error) {
      setCommandError(error instanceof Error ? error.message : "send dropped file failed");
    } finally {
      setBusyState((current) => ({ ...current, sendingFile: false }));
    }
  }

  useEffect(() => {
    const runtime = readWailsDropRuntime();
    if (!runtime?.OnFileDrop || !resolvedApi.sendFilePath) {
      return;
    }

    runtime.OnFileDrop((_x, _y, paths) => {
      const path = paths[0];
      if (!path) {
        return;
      }
      void handleSendFilePath(path);
    }, true);

    return () => {
      runtime.OnFileDropOff?.();
    };
  }, [
    busyState.pickingLocalFile,
    busyState.sendingAcceleratedFile,
    busyState.sendingFile,
    resolvedApi,
    selectedPeer,
  ]);

  async function handlePickLocalFile() {
    if (!selectedPeer) {
      return;
    }

    setCommandError(undefined);
    setBusyState((current) => ({ ...current, pickingLocalFile: true }));
    try {
      const localFile = await resolvedApi.pickLocalFile();
      setPickedLocalFile(localFile);
    } catch (error) {
      setCommandError(error instanceof Error ? error.message : "pick local file failed");
    } finally {
      setBusyState((current) => ({ ...current, pickingLocalFile: false }));
    }
  }

  async function handleSendAcceleratedFile() {
    if (!selectedPeer || !pickedLocalFile) {
      return;
    }

    setCommandError(undefined);
    setBusyState((current) => ({ ...current, sendingAcceleratedFile: true }));
    try {
      const transfer = await resolvedApi.sendAcceleratedFile(selectedPeer.deviceId, pickedLocalFile.localFileId);
      startTransition(() => {
        setSnapshot((current) =>
          current ? upsertOutgoingPickedFile(current, selectedPeer.deviceId, pickedLocalFile, transfer) : current,
        );
      });
      setPickedLocalFile(null);
    } catch (error) {
      setCommandError(error instanceof Error ? error.message : "accelerated send failed");
    } finally {
      setBusyState((current) => ({ ...current, sendingAcceleratedFile: false }));
    }
  }

  async function handleLoadOlderMessages() {
    if (!selectedConversation) {
      return;
    }

    const conversationId = selectedConversation.conversationId;
    const historyState = historyStateByConversation[conversationId];
    const beforeCursor = historyState?.nextCursor ?? selectedConversation.nextCursor;
    const hasMore = historyState?.hasMore ?? Boolean(selectedConversation.hasMoreHistory);
    if (!beforeCursor || !hasMore || historyState?.loading) {
      return;
    }

    setHistoryStateByConversation((current) => ({
      ...current,
      [conversationId]: {
        olderMessages: current[conversationId]?.olderMessages ?? [],
        hasMore,
        nextCursor: beforeCursor,
        loading: true,
        error: undefined,
      },
    }));

    try {
      const page = await resolvedApi.listMessageHistory(conversationId, beforeCursor);
      startTransition(() => {
        setHistoryStateByConversation((current) => {
          const previous = current[conversationId];
          const olderMessages = mergeHistoryMessages(previous?.olderMessages ?? [], page.messages);
          return {
            ...current,
            [conversationId]: {
              olderMessages,
              hasMore: page.hasMore,
              nextCursor: page.nextCursor,
              loading: false,
              error: undefined,
            },
          };
        });
      });
    } catch (error) {
      setHistoryStateByConversation((current) => ({
        ...current,
        [conversationId]: {
          olderMessages: current[conversationId]?.olderMessages ?? [],
          hasMore,
          nextCursor: beforeCursor,
          loading: false,
          error: error instanceof Error ? error.message : "load message history failed",
        },
      }));
    }
  }

  if (errorMessage) {
    return (
      <main className="ms-app">
        <div className="ms-shell">
          <section className="ms-splash ms-splash--error">
            <span className="ms-eyebrow">Connection Error</span>
            <h1 className="ms-splash__title">无法连接本机服务</h1>
            <p className="ms-splash__body">{errorMessage}</p>
          </section>
        </div>
      </main>
    );
  }

  if (!snapshot) {
    return (
      <main className="ms-app">
        <div className="ms-shell">
          <section className="ms-splash">
            <span className="ms-eyebrow">LAN P2P Share</span>
            <h1 className="ms-splash__title">一页直传</h1>
            <p className="ms-splash__body">正在连接本机 shareme 服务</p>
          </section>
        </div>
      </main>
    );
  }

  const discoveredCount = peers.length;
  const readyCount = peers.filter((peer) => peer.trusted && peer.reachable).length;

  function handleSelectPeer(peer: PeerSummary) {
    setSelectedPeerId(peer.deviceId);
    setPickedLocalFile(null);
    if (
      typeof window !== "undefined" &&
      typeof window.matchMedia === "function" &&
      window.matchMedia("(max-width: 940px)").matches
    ) {
      window.requestAnimationFrame(() => {
        mainColumnRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
      });
    }
  }

  return (
    <div className="ms-app">
      <div className="ms-shell">
        <header className="ms-appbar" role="banner" aria-label="shareme 工作台">
          <div className="ms-appbar__identity">
            <span className="ms-appbar__mark" aria-hidden="true">MS</span>
            <div>
              <span className="ms-eyebrow">shareme</span>
              <strong className="ms-appbar__title">传输工作台</strong>
              <span className="ms-appbar__copy">文字与文件都会直连传输，不经过云端。</span>
            </div>
          </div>
          <div className="ms-appbar__meta">
            <span className="ms-chip ms-chip--soft">本机设备</span>
            <strong>{snapshot.localDeviceName}</strong>
            <span className="ms-chip ms-chip--soft">已发现 {discoveredCount}</span>
            <span className="ms-chip ms-chip--soft">
              <span>已信任且可连接</span>
              <strong className="ms-chip__count">{readyCount}</strong>
            </span>
          </div>
        </header>

        {commandError ? (
          <section className="ms-command-error" role="alert">
            {commandError}
          </section>
        ) : null}

        <section
          className={`ms-workbench${deviceDockCollapsed ? " is-dock-collapsed" : ""}`}
          aria-label="传输工作台"
        >
          <DiscoveryPage
            peers={peers}
            selectedPeerId={selectedPeer?.deviceId}
            onSelect={handleSelectPeer}
            localDeviceName={snapshot.localDeviceName}
            syncMode="点对点即时传输"
            collapsed={deviceDockCollapsed}
            onToggleCollapsed={() => setDeviceDockCollapsed((current) => !current)}
          />

          <main className="ms-main-column" ref={mainColumnRef} aria-label="会话工作区">
            <ChatPane
              peer={selectedPeer}
              conversationId={selectedConversation?.conversationId}
              messages={selectedMessages}
              sendingText={busyState.sendingText}
              sendingFile={busyState.sendingFile}
              pickingLocalFile={busyState.pickingLocalFile}
              sendingAcceleratedFile={busyState.sendingAcceleratedFile}
              pickedLocalFile={pickedLocalFile}
              historyHasMore={selectedHistoryHasMore}
              historyLoading={selectedHistoryLoading}
              historyError={selectedHistoryError}
              onSendText={handleSendText}
              onSendFile={handleSendFile}
              onPickLocalFile={handlePickLocalFile}
              onSendAcceleratedFile={handleSendAcceleratedFile}
              onLoadOlderMessages={handleLoadOlderMessages}
            />
            <PairCodeDialog
              peer={selectedPeer}
              busy={busyState.startingPairing || busyState.confirmingPairing}
              onStartPairing={handleStartPairing}
              onConfirmPairing={handleConfirmPairing}
            />
          </main>

          <WorkbenchStatusPanel
            health={snapshot.health}
            lastEventSeq={snapshot.eventSeq ?? 0}
            transfers={activeTransfers}
          />
        </section>
      </div>
    </div>
  );
}

function applyEvent(current: BootstrapSnapshot | null, event: AgentEvent): BootstrapSnapshot | null {
  if (!current) {
    return current;
  }

  if (event.kind === "health.updated") {
    return {
      ...current,
      health: {
        ...current.health,
        ...(event.payload as Partial<typeof current.health>),
      },
      eventSeq: event.eventSeq,
    };
  }

  if (event.kind === "peer.updated") {
    return {
      ...upsertPeer(current, event.payload as Partial<PeerSnapshot> & { deviceId: string }),
      eventSeq: event.eventSeq,
    };
  }

  if (event.kind === "pairing.updated") {
    return {
      ...upsertPairing(current, event.payload as PairingSnapshot),
      eventSeq: event.eventSeq,
    };
  }

  if (event.kind === "message.upserted") {
    const message = event.payload as MessageSnapshot;
    let next = current;
    const peerDeviceId = parsePeerDeviceIdFromConversation(message.conversationId);
    if (peerDeviceId) {
      next = ensureConversation(next, peerDeviceId, message.conversationId);
    }
    next = upsertMessage(next, message);
    return { ...next, eventSeq: event.eventSeq };
  }

  if (event.kind === "transfer.updated") {
    return {
      ...upsertTransfer(current, event.payload as TransferSnapshot),
      eventSeq: event.eventSeq,
    };
  }

  return {
    ...current,
    eventSeq: Math.max(current.eventSeq ?? 0, event.eventSeq),
  };
}

function reconcileBootstrapSnapshot(
  current: BootstrapSnapshot | null,
  nextSnapshot: BootstrapSnapshot,
): BootstrapSnapshot {
  if (!current) {
    return nextSnapshot;
  }

  const currentEventSeq = current.eventSeq ?? 0;
  const nextEventSeq = nextSnapshot.eventSeq ?? 0;
  if (nextEventSeq < currentEventSeq) {
    return current;
  }

  return nextSnapshot;
}

function pickDefaultPeerId(snapshot: BootstrapSnapshot): string | undefined {
  return buildPeerSummaries(snapshot)[0]?.deviceId;
}

function buildPeerSummaries(snapshot: BootstrapSnapshot): PeerSummary[] {
  const latestMessages = new Map<string, MessageSnapshot>();
  for (const message of snapshot.messages) {
    const current = latestMessages.get(message.conversationId);
    if (!current || compareConversationMessage(current, message) < 0) {
      latestMessages.set(message.conversationId, message);
    }
  }

  const conversationsByPeer = new Map<string, ConversationSnapshot>();
  for (const conversation of snapshot.conversations) {
    conversationsByPeer.set(conversation.peerDeviceId, conversation);
  }

  const pairingsByPeer = new Map<string, PairingSnapshot>();
  for (const pairing of snapshot.pairings) {
    const current = pairingsByPeer.get(pairing.peerDeviceId);
    if (!current || pairingPriority(pairing.status) > pairingPriority(current.status)) {
      pairingsByPeer.set(pairing.peerDeviceId, pairing);
    }
  }

  return [...snapshot.peers]
    .map((peer) => {
      const conversation = conversationsByPeer.get(peer.deviceId);
      const preview = conversation
        ? formatPeerPreview(latestMessages.get(conversation.conversationId))
        : undefined;

      return {
        ...peer,
        pairing: pairingsByPeer.get(peer.deviceId),
        lastMessagePreview: preview,
      };
    })
    .sort((left, right) => {
      const priorityGap = peerPriority(left) - peerPriority(right);
      if (priorityGap !== 0) {
        return priorityGap;
      }
      return left.deviceName.localeCompare(right.deviceName, "zh-CN");
    });
}

function formatPeerPreview(message?: MessageSnapshot): string | undefined {
  if (!message) {
    return undefined;
  }
  if (message.kind === "file") {
    return message.direction === "incoming" ? "收到一个文件" : "发送了一个文件";
  }
  return message.body;
}

function pairingPriority(status: string): number {
  if (status === "confirmed") {
    return 3;
  }
  if (status === "pending") {
    return 2;
  }
  if (status === "failed") {
    return 1;
  }
  return 0;
}

function peerPriority(peer: PeerSummary): number {
  if (peer.trusted && peer.reachable) {
    return 0;
  }
  if (!peer.trusted && peer.reachable) {
    return 1;
  }
  if (peer.trusted) {
    return 2;
  }
  return 3;
}

function buildConversationMessages(
  snapshot: BootstrapSnapshot,
  peerDeviceId: string,
  olderMessages: MessageSnapshot[] = [],
): ConversationMessage[] {
  const conversationId = resolveConversationId(snapshot, peerDeviceId);
  if (!conversationId) {
    return [];
  }

  const transfersByMessageId = new Map<string, TransferSnapshot>();
  for (const transfer of snapshot.transfers) {
    transfersByMessageId.set(transfer.messageId, transfer);
  }

  const messagesByID = new Map<string, MessageSnapshot>();
  for (const message of olderMessages) {
    if (message.conversationId === conversationId) {
      messagesByID.set(message.messageId, message);
    }
  }
  for (const message of snapshot.messages) {
    if (message.conversationId === conversationId) {
      messagesByID.set(message.messageId, message);
    }
  }

  return [...messagesByID.values()]
    .sort(compareConversationMessage)
    .map((message) => ({
      ...message,
      transfer: transfersByMessageId.get(message.messageId),
    }));
}

function resolveConversationId(snapshot: BootstrapSnapshot, peerDeviceId: string): string | undefined {
  return snapshot.conversations.find((conversation) => conversation.peerDeviceId === peerDeviceId)?.conversationId;
}

function ensureConversation(
  snapshot: BootstrapSnapshot,
  peerDeviceId: string,
  conversationId = `conv-${peerDeviceId}`,
): BootstrapSnapshot {
  if (snapshot.conversations.some((conversation) => conversation.conversationId === conversationId)) {
    return snapshot;
  }

  return {
    ...snapshot,
    conversations: [
      ...snapshot.conversations,
      {
        conversationId,
        peerDeviceId,
        peerDeviceName:
          snapshot.peers.find((peer) => peer.deviceId === peerDeviceId)?.deviceName ?? peerDeviceId,
        hasMoreHistory: false,
        nextCursor: "",
      },
    ],
  };
}

function mergeHistoryMessages(current: MessageSnapshot[], incoming: MessageSnapshot[]): MessageSnapshot[] {
  const messagesByID = new Map<string, MessageSnapshot>();
  for (const message of current) {
    messagesByID.set(message.messageId, message);
  }
  for (const message of incoming) {
    if (!messagesByID.has(message.messageId)) {
      messagesByID.set(message.messageId, message);
    }
  }
  return [...messagesByID.values()].sort(compareConversationMessage);
}

function upsertPeer(
  snapshot: BootstrapSnapshot,
  nextPeer: Partial<PeerSnapshot> & { deviceId: string },
): BootstrapSnapshot {
  const existing = snapshot.peers.find((peer) => peer.deviceId === nextPeer.deviceId);
  const peers = existing
    ? snapshot.peers.map((peer) =>
        peer.deviceId === nextPeer.deviceId ? { ...peer, ...nextPeer } : peer,
      )
    : [
        ...snapshot.peers,
        {
          deviceId: nextPeer.deviceId,
          deviceName: nextPeer.deviceName ?? nextPeer.deviceId,
          trusted: nextPeer.trusted ?? false,
          online: nextPeer.online ?? false,
          reachable: nextPeer.reachable ?? false,
          agentTcpPort: nextPeer.agentTcpPort,
          lastKnownAddr: nextPeer.lastKnownAddr,
        },
      ];

  return {
    ...snapshot,
    peers,
  };
}

function upsertPairing(snapshot: BootstrapSnapshot, pairing: PairingSnapshot): BootstrapSnapshot {
  const pairings = snapshot.pairings.some((current) => current.pairingId === pairing.pairingId)
    ? snapshot.pairings.map((current) =>
        current.pairingId === pairing.pairingId ? pairing : current,
      )
    : [...snapshot.pairings, pairing];

  return {
    ...snapshot,
    pairings,
  };
}

function upsertMessage(snapshot: BootstrapSnapshot, message: MessageSnapshot): BootstrapSnapshot {
  const messages = snapshot.messages.some((current) => current.messageId === message.messageId)
    ? snapshot.messages.map((current) =>
        current.messageId === message.messageId ? message : current,
      )
    : [...snapshot.messages, message];

  return {
    ...snapshot,
    messages,
  };
}

function upsertTransfer(snapshot: BootstrapSnapshot, transfer: TransferSnapshot): BootstrapSnapshot {
  const transfers = snapshot.transfers.some((current) => current.transferId === transfer.transferId)
    ? snapshot.transfers.map((current) =>
        current.transferId === transfer.transferId ? transfer : current,
      )
    : [...snapshot.transfers, transfer];

  return {
    ...snapshot,
    transfers,
  };
}

function upsertMessageForPeer(
  snapshot: BootstrapSnapshot,
  peerDeviceId: string,
  message: MessageSnapshot,
): BootstrapSnapshot {
  return upsertMessage(ensureConversation(snapshot, peerDeviceId, message.conversationId), message);
}

function upsertOutgoingFile(
  snapshot: BootstrapSnapshot,
  peerDeviceId: string,
  transfer: TransferSnapshot,
): BootstrapSnapshot {
  const conversationId = resolveConversationId(snapshot, peerDeviceId) ?? `conv-${peerDeviceId}`;
  const nextSnapshot = ensureConversation(snapshot, peerDeviceId, conversationId);
  const withTransfer = upsertTransfer(nextSnapshot, transfer);

  return upsertMessage(withTransfer, {
    messageId: transfer.messageId,
    conversationId,
    direction: "outgoing",
    kind: "file",
    body: transfer.fileName || "文件",
    status: transfer.state === "done" ? "sent" : transfer.state,
    createdAt: transfer.createdAt,
  });
}

function upsertOutgoingPickedFile(
  snapshot: BootstrapSnapshot,
  peerDeviceId: string,
  pickedLocalFile: LocalFileSnapshot,
  transfer: TransferSnapshot,
): BootstrapSnapshot {
  const conversationId = resolveConversationId(snapshot, peerDeviceId) ?? `conv-${peerDeviceId}`;
  const nextSnapshot = ensureConversation(snapshot, peerDeviceId, conversationId);
  const withTransfer = upsertTransfer(nextSnapshot, transfer);

  return upsertMessage(withTransfer, {
    messageId: transfer.messageId,
    conversationId,
    direction: "outgoing",
    kind: "file",
    body: transfer.fileName || pickedLocalFile.displayName,
    status: transfer.state === "done" ? "sent" : transfer.state,
    createdAt: transfer.createdAt,
  });
}

function readWailsDropRuntime(): WailsDropRuntime | undefined {
  if (typeof window === "undefined") {
    return undefined;
  }
  return (window as Window & { runtime?: WailsDropRuntime }).runtime;
}

function parsePeerDeviceIdFromConversation(conversationId: string): string | undefined {
  if (!conversationId.startsWith("conv-")) {
    return undefined;
  }
  return conversationId.slice(5);
}

function compareCreatedAt(left: string, right: string): number {
  const leftValue = Date.parse(left);
  const rightValue = Date.parse(right);

  const leftValid = Number.isFinite(leftValue);
  const rightValid = Number.isFinite(rightValue);
  if (leftValid && rightValid) {
    return leftValue - rightValue;
  }
  return left.localeCompare(right);
}

function compareConversationMessage(left: MessageSnapshot, right: MessageSnapshot): number {
  const createdAtGap = compareCreatedAt(left.createdAt, right.createdAt);
  if (createdAtGap !== 0) {
    return createdAtGap;
  }
  return left.messageId.localeCompare(right.messageId, "en");
}
