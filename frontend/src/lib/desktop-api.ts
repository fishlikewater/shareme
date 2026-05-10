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

const DESKTOP_EVENT_NAME = "shareme:event";

export type DesktopCommands = {
  Bootstrap: () => Promise<BootstrapSnapshot>;
  StartPairing: (peerDeviceId: string) => Promise<PairingSnapshot>;
  ConfirmPairing: (pairingId: string) => Promise<PairingSnapshot>;
  SendText: (peerDeviceId: string, body: string) => Promise<MessageSnapshot>;
  SendFile: (peerDeviceId: string) => Promise<TransferSnapshot>;
  PickLocalFile: () => Promise<LocalFileSnapshot>;
  SendAcceleratedFile: (peerDeviceId: string, localFileId: string) => Promise<TransferSnapshot>;
  ListMessageHistory: (conversationId: string, beforeCursor?: string) => Promise<MessageHistoryPage>;
  ReplayEvents?: (afterSeq: number) => Promise<AgentEvent[]>;
  UiReady?: () => Promise<void>;
};

type EventsOn = (eventName: string, callback: (event: AgentEvent) => void) => (() => void) | void;
type EventsOff = (eventName: string) => void;

type DesktopApiDependencies = {
  commands?: DesktopCommands;
  eventsOn?: EventsOn;
  eventsOff?: EventsOff;
};

type DesktopWindow = Window & {
  go?: {
    main?: {
      DesktopApp?: DesktopCommands;
    };
  };
  runtime?: {
    EventsOn?: EventsOn;
    EventsOff?: EventsOff;
  };
};

export function createDesktopApiClient(dependencies: DesktopApiDependencies = {}): LocalApi {
  const commands = dependencies.commands ?? resolveDesktopCommands();
  const eventsOn = dependencies.eventsOn ?? resolveDesktopEventsOn();
  const eventsOff = dependencies.eventsOff ?? resolveDesktopEventsOff();

  return {
    bootstrap: () => commands.Bootstrap(),
    startPairing: (peerDeviceId) => commands.StartPairing(peerDeviceId),
    confirmPairing: (pairingId) => commands.ConfirmPairing(pairingId),
    sendText: (peerDeviceId, body) => commands.SendText(peerDeviceId, body),
    sendFile: (peerDeviceId) => commands.SendFile(peerDeviceId),
    pickLocalFile: () => commands.PickLocalFile(),
    sendAcceleratedFile: (peerDeviceId, localFileId) => commands.SendAcceleratedFile(peerDeviceId, localFileId),
    listMessageHistory: (conversationId, beforeCursor) => commands.ListMessageHistory(conversationId, beforeCursor),
    subscribeEvents({ lastEventSeq = 0, onEvent }) {
      let cursor = lastEventSeq;
      let closed = false;
      let liveMode = false;
      const queuedEvents: AgentEvent[] = [];
      const unsubscribe = eventsOn(DESKTOP_EVENT_NAME, (event) => {
        if (closed) {
          return;
        }
        if (!liveMode) {
          queuedEvents.push(event);
          return;
        }
        deliverEvent(event);
      });

      void replayDesktopEvents(commands, cursor)
        .then((events) => {
          if (closed) {
            return;
          }
          for (const event of sortEvents(events)) {
            deliverEvent(event);
          }
        })
        .finally(() => {
          if (closed) {
            return;
          }
          liveMode = true;
          for (const event of sortEvents(queuedEvents.splice(0))) {
            deliverEvent(event);
          }
        });

      function deliverEvent(event: AgentEvent) {
        if (event.eventSeq <= cursor) {
          return;
        }
        cursor = event.eventSeq;
        onEvent(event);
      }

      return {
        close() {
          closed = true;
          if (typeof unsubscribe === "function") {
            unsubscribe();
          }
          eventsOff(DESKTOP_EVENT_NAME);
        },
        reconnect() {},
      };
    },
  };
}

export async function notifyDesktopUiReady(): Promise<void> {
  const command = tryResolveDesktopUiReady();
  if (typeof command === "function") {
    await command();
  }
}

export function hasDesktopApiBindings(): boolean {
  return tryResolveDesktopCommands() !== undefined && tryResolveDesktopEventsOn() !== undefined;
}

async function replayDesktopEvents(commands: DesktopCommands, afterSeq: number): Promise<AgentEvent[]> {
  if (typeof commands.ReplayEvents !== "function") {
    return [];
  }
  return commands.ReplayEvents(afterSeq);
}

function sortEvents(events: AgentEvent[]): AgentEvent[] {
  return [...events].sort((left, right) => left.eventSeq - right.eventSeq);
}

function resolveDesktopCommands(): DesktopCommands {
  const commands = tryResolveDesktopCommands();
  if (!commands) {
    throw new Error("desktop api commands not available");
  }
  return commands;
}

function resolveDesktopEventsOn(): EventsOn {
  const eventsOn = tryResolveDesktopEventsOn();
  if (!eventsOn) {
    throw new Error("desktop event bridge not available");
  }
  return eventsOn;
}

function resolveDesktopEventsOff(): EventsOff {
  return tryResolveDesktopEventsOff() ?? (() => {});
}

function tryResolveDesktopCommands(): DesktopCommands | undefined {
  const desktopWindow = readDesktopWindow();
  return desktopWindow?.go?.main?.DesktopApp;
}

function tryResolveDesktopUiReady(): (() => Promise<void>) | undefined {
  return tryResolveDesktopCommands()?.UiReady;
}

function tryResolveDesktopEventsOn(): EventsOn | undefined {
  const desktopWindow = readDesktopWindow();
  return desktopWindow?.runtime?.EventsOn;
}

function tryResolveDesktopEventsOff(): EventsOff | undefined {
  const desktopWindow = readDesktopWindow();
  return desktopWindow?.runtime?.EventsOff;
}

function readDesktopWindow(): DesktopWindow | undefined {
  if (typeof window === "undefined") {
    return undefined;
  }
  return window as DesktopWindow;
}
