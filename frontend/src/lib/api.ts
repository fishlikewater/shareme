import { createDesktopApiClient, hasDesktopApiBindings } from "./desktop-api";
import { createLocalhostApiClient } from "./localhost-api";
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
  sendFile: (peerDeviceId: string, file?: File) => Promise<TransferSnapshot>;
  sendFilePath?: (peerDeviceId: string, path: string) => Promise<TransferSnapshot>;
  pickLocalFile: () => Promise<LocalFileSnapshot>;
  sendAcceleratedFile: (peerDeviceId: string, localFileId: string) => Promise<TransferSnapshot>;
  listMessageHistory: (conversationId: string, beforeCursor?: string) => Promise<MessageHistoryPage>;
  subscribeEvents: (options: {
    lastEventSeq?: number;
    onEvent: (event: AgentEvent) => void;
  }) => EventSubscription;
}

export function createDefaultLocalApi(): LocalApi {
  if (hasDesktopApiBindings()) {
    return createDesktopApiClient();
  }
  if (isLoopbackBrowser()) {
    return createLocalhostApiClient({
      origin: window.location.origin,
    });
  }
  throw new Error("local api bindings not available");
}

function isLoopbackBrowser(): boolean {
  if (typeof window === "undefined") {
    return false;
  }

  return new Set(["localhost", "127.0.0.1", "[::1]"]).has(window.location.hostname);
}
