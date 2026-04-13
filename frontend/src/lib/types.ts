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
};

export type MessageSnapshot = {
  messageId: string;
  conversationId: string;
  direction: string;
  kind: string;
  body: string;
  status: string;
  createdAt: string;
};

export type TransferSnapshot = {
  transferId: string;
  messageId: string;
  fileName: string;
  fileSize: number;
  state: string;
  createdAt: string;
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
