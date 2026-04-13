import { startTransition, useEffect, useMemo, useRef, useState } from "react";

import { ChatPane } from "./components/ChatPane";
import { HealthBanner } from "./components/HealthBanner";
import { PairCodeDialog } from "./components/PairCodeDialog";
import { createLocalApiClient, type LocalApi } from "./lib/api";
import type {
  AgentEvent,
  BootstrapSnapshot,
  ConversationMessage,
  ConversationSnapshot,
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
};

const initialBusyState: BusyState = {
  startingPairing: false,
  confirmingPairing: false,
  sendingText: false,
  sendingFile: false,
};

const SNAPSHOT_REFRESH_INTERVAL_MS = 3000;

export default function AppShell({ api }: AppProps) {
  const [defaultApi] = useState<LocalApi | undefined>(() => (api ? undefined : createLocalApiClient()));
  const resolvedApi = api ?? defaultApi!;
  const mainColumnRef = useRef<HTMLElement | null>(null);
  const [snapshot, setSnapshot] = useState<BootstrapSnapshot | null>(null);
  const [selectedPeerId, setSelectedPeerId] = useState<string>();
  const [errorMessage, setErrorMessage] = useState<string>();
  const [commandError, setCommandError] = useState<string>();
  const [busyState, setBusyState] = useState<BusyState>(initialBusyState);

  useEffect(() => {
    let subscription: ReturnType<LocalApi["subscribeEvents"]> | undefined;
    let disposed = false;
    let loading = false;

    const applySnapshot = (nextSnapshot: BootstrapSnapshot) => {
      setErrorMessage(undefined);
      setSnapshot(nextSnapshot);
      setSelectedPeerId((current) =>
        current && nextSnapshot.peers.some((peer) => peer.deviceId === current)
          ? current
          : pickDefaultPeerId(nextSnapshot),
      );
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

        if (subscribeAfterLoad && !subscription) {
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

  const peers = useMemo(() => (snapshot ? buildPeerSummaries(snapshot) : []), [snapshot]);
  const selectedPeer = useMemo(
    () => peers.find((peer) => peer.deviceId === selectedPeerId),
    [peers, selectedPeerId],
  );
  const selectedMessages = useMemo(
    () => (snapshot && selectedPeer ? buildConversationMessages(snapshot, selectedPeer.deviceId) : []),
    [selectedPeer, snapshot],
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

  async function handleSendFile(file: File) {
    if (!selectedPeer) {
      return;
    }

    setCommandError(undefined);
    setBusyState((current) => ({ ...current, sendingFile: true }));
    try {
      const transfer = await resolvedApi.sendFile(selectedPeer.deviceId, file);
      startTransition(() => {
        setSnapshot((current) =>
          current ? upsertOutgoingFile(current, selectedPeer.deviceId, file, transfer) : current,
        );
      });
    } catch (error) {
      setCommandError(error instanceof Error ? error.message : "send file failed");
    } finally {
      setBusyState((current) => ({ ...current, sendingFile: false }));
    }
  }

  if (errorMessage) {
    return (
      <main className="ms-app">
        <div className="ms-shell">
          <section className="ms-splash ms-splash--error">
            <span className="ms-eyebrow">Connection Error</span>
            <h1 className="ms-splash__title">无法连接本机代理</h1>
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
            <p className="ms-splash__body">正在连接本机代理</p>
          </section>
        </div>
      </main>
    );
  }

  const discoveredCount = peers.length;
  const trustedCount = peers.filter((peer) => peer.trusted).length;
  const readyCount = peers.filter((peer) => peer.trusted && peer.reachable).length;
  const pendingCount = peers.filter((peer) => !peer.trusted).length;

  function handleSelectPeer(peer: PeerSummary) {
    setSelectedPeerId(peer.deviceId);
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
    <main className="ms-app">
      <div className="ms-shell">
        <section className="ms-hero">
          <section className="ms-panel ms-hero__lead">
            <span className="ms-eyebrow">LAN P2P Share</span>
            <h1 className="ms-hero__title">一页直传</h1>
            <p className="ms-hero__body">文字与文件都会直连传输，不经过云端。</p>
            <div className="ms-hero__device">
              <span className="ms-chip ms-chip--soft">本机设备</span>
              <strong>{snapshot.localDeviceName}</strong>
            </div>
          </section>

          <section className="ms-hero__side">
            <div className="ms-stat-grid">
              <article className="ms-stat-card">
                <span className="ms-stat-card__label">已发现</span>
                <strong className="ms-stat-card__value">{discoveredCount}</strong>
                <span className="ms-stat-card__hint">局域网设备</span>
              </article>
              <article className="ms-stat-card">
                <span className="ms-stat-card__label">已配对</span>
                <strong className="ms-stat-card__value">{trustedCount}</strong>
                <span className="ms-stat-card__hint">可建立信任</span>
              </article>
              <article className="ms-stat-card">
                <span className="ms-stat-card__label">可直传</span>
                <strong className="ms-stat-card__value">{readyCount}</strong>
                <span className="ms-stat-card__hint">在线且可达</span>
              </article>
              <article className="ms-stat-card">
                <span className="ms-stat-card__label">待配对</span>
                <strong className="ms-stat-card__value">{pendingCount}</strong>
                <span className="ms-stat-card__hint">先建立信任</span>
              </article>
            </div>
          </section>
        </section>

        <HealthBanner health={snapshot.health} lastEventSeq={snapshot.eventSeq ?? 0} />
        {commandError ? (
          <section className="ms-command-error" role="alert">
            {commandError}
          </section>
        ) : null}

        <section className="ms-layout">
          <DiscoveryPage
            peers={peers}
            selectedPeerId={selectedPeer?.deviceId}
            onSelect={handleSelectPeer}
            localDeviceName={snapshot.localDeviceName}
            syncMode="点对点即时传输"
          />

          <section className="ms-main-column" ref={mainColumnRef}>
            <ChatPane
              peer={selectedPeer}
              messages={selectedMessages}
              sendingText={busyState.sendingText}
              sendingFile={busyState.sendingFile}
              onSendText={handleSendText}
              onSendFile={handleSendFile}
            />
            <PairCodeDialog
              peer={selectedPeer}
              busy={busyState.startingPairing || busyState.confirmingPairing}
              onStartPairing={handleStartPairing}
              onConfirmPairing={handleConfirmPairing}
            />
          </section>
        </section>
      </div>
    </main>
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

function pickDefaultPeerId(snapshot: BootstrapSnapshot): string | undefined {
  return buildPeerSummaries(snapshot)[0]?.deviceId;
}

function buildPeerSummaries(snapshot: BootstrapSnapshot): PeerSummary[] {
  const latestMessages = new Map<string, MessageSnapshot>();
  for (const message of snapshot.messages) {
    const current = latestMessages.get(message.conversationId);
    if (!current || compareCreatedAt(current.createdAt, message.createdAt) < 0) {
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
      const preview = conversation ? latestMessages.get(conversation.conversationId)?.body : undefined;

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
): ConversationMessage[] {
  const conversationId = resolveConversationId(snapshot, peerDeviceId);
  if (!conversationId) {
    return [];
  }

  const transfersByMessageId = new Map<string, TransferSnapshot>();
  for (const transfer of snapshot.transfers) {
    transfersByMessageId.set(transfer.messageId, transfer);
  }

  return [...snapshot.messages]
    .filter((message) => message.conversationId === conversationId)
    .sort((left, right) => compareCreatedAt(left.createdAt, right.createdAt))
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
      },
    ],
  };
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
  file: File,
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
    body: transfer.fileName || file.name,
    status: transfer.state === "done" ? "sent" : transfer.state,
    createdAt: transfer.createdAt,
  });
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
