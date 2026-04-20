import type { LocalApi } from "./api";
import type {
  AgentEvent,
  BootstrapSnapshot,
  LocalFileSnapshot,
  MessageHistoryPage,
  MessageSnapshot,
  PairingSnapshot,
  TransferSnapshot,
} from "./types";

type LocalhostApiDependencies = {
  origin?: string;
  fetchFn?: typeof fetch;
  createEventSource?: (url: string) => EventSource;
  pickFile?: () => Promise<File>;
};

export function createLocalhostApiClient(dependencies: LocalhostApiDependencies = {}): LocalApi {
  const origin = resolveOrigin(dependencies.origin);
  const fetchFn = dependencies.fetchFn ?? fetch;
  const createEventSource = dependencies.createEventSource ?? ((url) => new EventSource(url));
  const pickFile = dependencies.pickFile ?? pickBrowserFile;

  return {
    bootstrap: () => requestJson<BootstrapSnapshot>(fetchFn, origin, "/api/bootstrap"),
    startPairing: (peerDeviceId) =>
      requestJson<PairingSnapshot>(fetchFn, origin, "/api/pairings", {
        method: "POST",
        body: JSON.stringify({ peerDeviceId }),
      }),
    confirmPairing: (pairingId) =>
      requestJson<PairingSnapshot>(fetchFn, origin, `/api/pairings/${pairingId}/confirm`, {
        method: "POST",
      }),
    sendText: (peerDeviceId, body) =>
      requestJson<MessageSnapshot>(fetchFn, origin, `/api/peers/${peerDeviceId}/messages/text`, {
        method: "POST",
        body: JSON.stringify({ body }),
      }),
    async sendFile(peerDeviceId) {
      const file = await pickFile();
      const form = new FormData();
      form.set("fileSize", String(file.size));
      form.set("file", file, file.name);
      return requestForm<TransferSnapshot>(
        fetchFn,
        origin,
        `/api/peers/${peerDeviceId}/transfers/browser-upload`,
        form,
      );
    },
    pickLocalFile: () =>
      requestJson<LocalFileSnapshot>(fetchFn, origin, "/api/local-files/pick", {
        method: "POST",
      }),
    sendAcceleratedFile: (peerDeviceId, localFileId) =>
      requestJson<TransferSnapshot>(fetchFn, origin, `/api/peers/${peerDeviceId}/transfers/accelerated`, {
        method: "POST",
        body: JSON.stringify({ localFileId }),
      }),
    listMessageHistory: (conversationId, beforeCursor) =>
      requestJson<MessageHistoryPage>(
        fetchFn,
        origin,
        beforeCursor
          ? `/api/conversations/${conversationId}/messages?beforeCursor=${encodeURIComponent(beforeCursor)}`
          : `/api/conversations/${conversationId}/messages`,
      ),
    subscribeEvents({ lastEventSeq = 0, onEvent }) {
      let cursor = lastEventSeq;
      let closed = false;
      let source = open(cursor);

      function open(afterSeq: number): EventSource {
        const url = new URL("/api/events/stream", origin);
        url.searchParams.set("afterSeq", String(afterSeq));

        const nextSource = createEventSource(url.toString());
        nextSource.onmessage = (event) => {
          const payload = JSON.parse(event.data) as AgentEvent;
          if (payload.eventSeq <= cursor) {
            return;
          }
          cursor = payload.eventSeq;
          onEvent(payload);
        };
        return nextSource;
      }

      return {
        close() {
          closed = true;
          source.close();
        },
        reconnect() {
          if (closed) {
            return;
          }
          source.close();
          source = open(cursor);
        },
      };
    },
  };
}

function resolveOrigin(origin?: string): string {
  if (origin) {
    return origin;
  }
  if (typeof window === "undefined") {
    throw new Error("localhost api origin not available");
  }
  return window.location.origin;
}

async function requestJson<T>(
  fetchFn: typeof fetch,
  origin: string,
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const response = await fetchFn(joinURL(origin, path), {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init.headers ?? {}),
    },
  });
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return (await response.json()) as T;
}

async function requestForm<T>(fetchFn: typeof fetch, origin: string, path: string, body: FormData): Promise<T> {
  const response = await fetchFn(joinURL(origin, path), {
    method: "POST",
    body,
  });
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return (await response.json()) as T;
}

function joinURL(origin: string, path: string): string {
  return new URL(path, ensureTrailingSlash(origin)).toString();
}

function ensureTrailingSlash(value: string): string {
  return value.endsWith("/") ? value : `${value}/`;
}

async function pickBrowserFile(): Promise<File> {
  if (typeof document === "undefined") {
    throw new Error("browser file picker not available");
  }

  const input = document.createElement("input");
  input.type = "file";
  input.multiple = false;

  return new Promise<File>((resolve, reject) => {
    input.addEventListener(
      "change",
      () => {
        const file = input.files?.[0];
        if (!file) {
          reject(new Error("file selection cancelled"));
          return;
        }
        resolve(file);
      },
      { once: true },
    );
    input.click();
  });
}
