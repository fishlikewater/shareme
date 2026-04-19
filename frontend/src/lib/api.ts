import { createDesktopApiClient, hasDesktopApiBindings } from "./desktop-api";
import type {
  AgentEvent,
  BootstrapSnapshot,
  LocalFileSnapshot,
  MessageHistoryPage,
  MessageSnapshot,
  PairingSnapshot,
  TransferSnapshot,
} from "./types";

export interface EventSubscription {
  close: () => void;
  reconnect: () => void;
}

export interface LocalApi {
  bootstrap: () => Promise<BootstrapSnapshot>;
  startPairing: (peerDeviceId: string) => Promise<PairingSnapshot>;
  confirmPairing: (pairingId: string) => Promise<PairingSnapshot>;
  sendText: (peerDeviceId: string, body: string) => Promise<MessageSnapshot>;
  sendFile: (peerDeviceId: string) => Promise<TransferSnapshot>;
  pickLocalFile: () => Promise<LocalFileSnapshot>;
  sendAcceleratedFile: (peerDeviceId: string, localFileId: string) => Promise<TransferSnapshot>;
  listMessageHistory: (conversationId: string, beforeCursor?: string) => Promise<MessageHistoryPage>;
  subscribeEvents: (options: {
    lastEventSeq?: number;
    onEvent: (event: AgentEvent) => void;
  }) => EventSubscription;
}

export function createDefaultLocalApi(): LocalApi {
  if (!hasDesktopApiBindings()) {
    throw new Error("desktop api bindings not available");
  }
  return createDesktopApiClient();
}
