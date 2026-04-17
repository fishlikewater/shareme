import type {
  AgentEvent,
  BootstrapSnapshot,
  LocalFileSnapshot,
  MessageHistoryPage,
  MessageSnapshot,
  PairingSnapshot,
  TransferSnapshot,
} from "./types";

const DEFAULT_HTTP_BASE_URL = "http://127.0.0.1:19100";
const DEFAULT_WS_BASE_URL = "ws://127.0.0.1:19100";
const EVENT_RESYNC_INTERVAL_MS = 1500;
const EVENT_RECONNECT_DELAY_MS = 1000;

export interface EventSubscription {
  close: () => void;
  reconnect: () => void;
}

export interface LocalApi {
  bootstrap: () => Promise<BootstrapSnapshot>;
  startPairing: (peerDeviceId: string) => Promise<PairingSnapshot>;
  confirmPairing: (pairingId: string) => Promise<PairingSnapshot>;
  sendText: (peerDeviceId: string, body: string) => Promise<MessageSnapshot>;
  sendFile: (peerDeviceId: string, file: File) => Promise<TransferSnapshot>;
  pickLocalFile: () => Promise<LocalFileSnapshot>;
  sendAcceleratedFile: (peerDeviceId: string, localFileId: string) => Promise<TransferSnapshot>;
  listMessageHistory: (conversationId: string, beforeCursor?: string) => Promise<MessageHistoryPage>;
  subscribeEvents: (options: {
    lastEventSeq?: number;
    onEvent: (event: AgentEvent) => void;
  }) => EventSubscription;
}

type LocalApiClientOptions = {
  baseUrl?: string;
  wsBaseUrl?: string;
  fetchImpl?: typeof fetch;
  webSocketFactory?: (url: string) => EventSocketLike;
};

type EventSocketLike = {
  onmessage: ((event: MessageEvent<string>) => void) | null;
  onclose?: (() => void) | null;
  onerror?: (() => void) | null;
  close: () => void;
};

export function createLocalApiClient(options: LocalApiClientOptions = {}): LocalApi {
  const runtimeConfig = resolveRuntimeApiConfig();
  const baseUrl = options.baseUrl ?? runtimeConfig.baseUrl;
  const wsBaseUrl = options.wsBaseUrl ?? runtimeConfig.wsBaseUrl;
  const fetchImpl = options.fetchImpl ?? fetch;
  const webSocketFactory =
    options.webSocketFactory ??
    ((url: string) => new WebSocket(url) as unknown as EventSocketLike);

  return {
    async bootstrap() {
      return getJSON<BootstrapSnapshot>(fetchImpl, `${baseUrl}/api/bootstrap`, `bootstrap failed`);
    },
    async startPairing(peerDeviceId: string) {
      return postJSON<PairingSnapshot>(fetchImpl, `${baseUrl}/api/pairings`, {
        peerDeviceId,
      });
    },
    async confirmPairing(pairingId: string) {
      return postJSON<PairingSnapshot>(fetchImpl, `${baseUrl}/api/pairings/${pairingId}/confirm`, {});
    },
    async sendText(peerDeviceId: string, body: string) {
      return postJSON<MessageSnapshot>(fetchImpl, `${baseUrl}/api/messages/text`, {
        peerDeviceId,
        body,
      });
    },
    async sendFile(peerDeviceId: string, file: File) {
      const formData = new FormData();
      formData.append("peerDeviceId", peerDeviceId);
      formData.append("fileSize", String(file.size));
      formData.append("file", file);

      const response = await fetchImpl(`${baseUrl}/api/transfers/file`, {
        method: "POST",
        body: formData,
      });
      if (!response.ok) {
        throw new Error(await resolveErrorMessage(response, `send file failed: ${response.status}`));
      }

      return (await response.json()) as TransferSnapshot;
    },
    async pickLocalFile() {
      return postJSON<LocalFileSnapshot>(fetchImpl, `${baseUrl}/api/local-files/pick`);
    },
    async sendAcceleratedFile(peerDeviceId: string, localFileId: string) {
      return postJSON<TransferSnapshot>(fetchImpl, `${baseUrl}/api/transfers/accelerated`, {
        peerDeviceId,
        localFileId,
      });
    },
    async listMessageHistory(conversationId: string, beforeCursor?: string) {
      const url = new URL(`${baseUrl}/api/conversations/${encodeURIComponent(conversationId)}/messages`);
      if (beforeCursor) {
        url.searchParams.set("before", beforeCursor);
      }
      return getJSON<MessageHistoryPage>(fetchImpl, url.toString(), "list message history failed");
    },
    subscribeEvents({ lastEventSeq = 0, onEvent }) {
      let cursor = lastEventSeq;
      const ignoredSockets = new WeakSet<EventSocketLike>();
      let socket = openSocket(cursor);
      let closedByUser = false;
      let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
      const resyncTimer = window.setInterval(() => {
        void replayMissedEvents().catch(() => {});
      }, EVENT_RESYNC_INTERVAL_MS);

      function openSocket(nextSeq: number): EventSocketLike {
        const url = `${wsBaseUrl}/api/events/stream?lastEventSeq=${nextSeq}`;
        const nextSocket = webSocketFactory(url);
        nextSocket.onmessage = (event: MessageEvent<string>) => {
          const data = JSON.parse(String(event.data)) as AgentEvent;
          cursor = Math.max(cursor, data.eventSeq);
          onEvent(data);
        };
        nextSocket.onclose = () => {
          if (ignoredSockets.has(nextSocket) || socket !== nextSocket) {
            return;
          }
          scheduleReconnect();
        };
        nextSocket.onerror = () => {
          if (ignoredSockets.has(nextSocket) || socket !== nextSocket) {
            return;
          }
          scheduleReconnect();
        };
        return nextSocket;
      }

      function scheduleReconnect() {
        if (closedByUser || reconnectTimer) {
          return;
        }
        reconnectTimer = window.setTimeout(() => {
          reconnectTimer = undefined;
          void replayMissedEvents().catch(() => {}).finally(() => {
            if (!closedByUser) {
              socket = openSocket(cursor);
            }
          });
        }, EVENT_RECONNECT_DELAY_MS);
      }

      async function replayMissedEvents() {
        const response = await fetchImpl(`${baseUrl}/api/events?afterSeq=${cursor}`, { method: "GET" });
        if (!response.ok) {
          throw new Error(`events replay failed: ${response.status}`);
        }

        const payload = (await response.json()) as {
          events?: AgentEvent[];
          lastEventSeq?: number;
        };
        for (const event of payload.events ?? []) {
          if (event.eventSeq <= cursor) {
            continue;
          }
          cursor = event.eventSeq;
          onEvent(event);
        }
      }

      return {
        close() {
          closedByUser = true;
          if (reconnectTimer) {
            window.clearTimeout(reconnectTimer);
          }
          window.clearInterval(resyncTimer);
          socket.close();
        },
        reconnect() {
          if (reconnectTimer) {
            window.clearTimeout(reconnectTimer);
            reconnectTimer = undefined;
          }
          ignoredSockets.add(socket);
          socket.close();
          socket = openSocket(cursor);
        },
      };
    },
  };
}

export function createLocalApi(options: LocalApiClientOptions = {}): LocalApi {
  return createLocalApiClient(options);
}

async function getJSON<TResponse>(fetchImpl: typeof fetch, url: string, errorPrefix: string): Promise<TResponse> {
  const response = await fetchImpl(url, { method: "GET" });
  if (!response.ok) {
    throw new Error(await resolveErrorMessage(response, `${errorPrefix}: ${response.status}`));
  }
  return (await response.json()) as TResponse;
}

async function postJSON<TResponse>(
  fetchImpl: typeof fetch,
  url: string,
  payload?: Record<string, unknown>,
): Promise<TResponse> {
  const response = await fetchImpl(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: payload ? JSON.stringify(payload) : undefined,
  });

  if (!response.ok) {
    throw new Error(await resolveErrorMessage(response, `command failed: ${response.status}`));
  }

  return (await response.json()) as TResponse;
}

async function resolveErrorMessage(response: Response, fallback: string): Promise<string> {
  const text = (await response.text()).trim();
  if (text.length > 0) {
    return text;
  }
  return fallback;
}

function resolveRuntimeApiConfig(): { baseUrl: string; wsBaseUrl: string } {
  const queryLocalApi = typeof window === "undefined" ? "" : new URLSearchParams(window.location.search).get("localApi") ?? "";
  const envLocalApi = (import.meta.env.VITE_MESSAGE_SHARE_LOCAL_API_BASE_URL as string | undefined) ?? "";
  const envWsBase = (import.meta.env.VITE_MESSAGE_SHARE_LOCAL_API_WS_BASE_URL as string | undefined) ?? "";
  const sameOriginBaseUrl = resolveSameOriginBaseUrl();

  const baseUrl = normalizeBaseUrl(queryLocalApi || envLocalApi || sameOriginBaseUrl || DEFAULT_HTTP_BASE_URL);
  const wsBaseUrl = normalizeBaseUrl(envWsBase || deriveWebSocketBaseUrl(baseUrl) || DEFAULT_WS_BASE_URL);
  return { baseUrl, wsBaseUrl };
}

function resolveSameOriginBaseUrl(): string {
  if (typeof window === "undefined") {
    return "";
  }
  const { location } = window;
  if ((location.protocol !== "http:" && location.protocol !== "https:") || !isLoopbackHostname(location.hostname)) {
    return "";
  }
  return normalizeBaseUrl(location.origin);
}

function deriveWebSocketBaseUrl(baseUrl: string): string {
  try {
    const parsed = new URL(baseUrl);
    if (parsed.protocol === "https:") {
      parsed.protocol = "wss:";
    } else {
      parsed.protocol = "ws:";
    }
    return parsed.toString().replace(/\/$/, "");
  } catch {
    return DEFAULT_WS_BASE_URL;
  }
}

function normalizeBaseUrl(value: string): string {
  return value.trim().replace(/\/$/, "");
}

function isLoopbackHostname(hostname: string): boolean {
  const normalized = hostname.trim().toLowerCase();
  if (!normalized) {
    return false;
  }
  if (normalized === "localhost" || normalized.endsWith(".localhost")) {
    return true;
  }
  if (normalized === "::1") {
    return true;
  }
  return /^127(?:\.\d{1,3}){3}$/.test(normalized);
}
