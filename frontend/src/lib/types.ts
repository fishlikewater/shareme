type LooseString = string & {};

export type MessageDirection = "incoming" | "outgoing" | LooseString;
export type MessageKind = "text" | "file" | LooseString;
export type MessageStatus = "sending" | "sent" | "failed" | "done" | LooseString;
export type TransferState =
  | "preparing"
  | "sending"
  | "receiving"
  | "received"
  | "sent"
  | "done"
  | "failed"
  | "fallback_pending"
  | "fallback_transferring"
  | LooseString;

export type HealthSnapshot = {
  status: "ok" | "degraded" | "error" | string;
  discovery: string;
  localAPIReady: boolean;
  agentPort?: number;
  issues?: string[];
};

export type PeerSnapshot = {
  deviceId: string;
  deviceName: string;
  trusted: boolean;
  online: boolean;
  reachable: boolean;
  agentTcpPort?: number;
  lastKnownAddr?: string;
};

export type PairingSnapshot = {
  pairingId: string;
  peerDeviceId: string;
  peerDeviceName: string;
  shortCode: string;
  status: string;
};

export type ConversationSnapshot = {
  conversationId: string;
  peerDeviceId: string;
  peerDeviceName: string;
  hasMoreHistory?: boolean;
  nextCursor?: string;
};

export type MessageSnapshot = {
  messageId: string;
  conversationId: string;
  direction: MessageDirection;
  kind: MessageKind;
  body: string;
  status: MessageStatus;
  createdAt: string;
};

export type TransferSnapshot = {
  transferId: string;
  messageId: string;
  fileName: string;
  fileSize: number;
  state: TransferState;
  createdAt: string;
  direction: MessageDirection;
  bytesTransferred: number;
  progressPercent: number;
  rateBytesPerSec: number;
  etaSeconds: number | null;
  active: boolean;
};

export type LocalFileSnapshot = {
  localFileId: string;
  displayName: string;
  size: number;
  acceleratedEligible: boolean;
};

export type BootstrapSnapshot = {
  localDeviceName: string;
  health: HealthSnapshot;
  peers: PeerSnapshot[];
  pairings: PairingSnapshot[];
  conversations: ConversationSnapshot[];
  messages: MessageSnapshot[];
  transfers: TransferSnapshot[];
  eventSeq?: number;
};

export type MessageHistoryPage = {
  conversationId: string;
  messages: MessageSnapshot[];
  hasMore: boolean;
  nextCursor?: string;
};

export type AgentEvent = {
  eventSeq: number;
  kind:
    | "health.updated"
    | "peer.updated"
    | "pairing.updated"
    | "message.upserted"
    | "transfer.updated"
    | string;
  payload: Record<string, unknown>;
};

export type PeerSummary = PeerSnapshot & {
  pairing?: PairingSnapshot;
  lastMessagePreview?: string;
};

export type ConversationMessage = MessageSnapshot & {
  transfer?: TransferSnapshot;
};
