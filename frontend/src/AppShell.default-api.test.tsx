import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { BootstrapSnapshot } from "./lib/types";

const createLocalApiClient = vi.fn();

vi.mock("./lib/api", async () => {
  const actual = await vi.importActual<typeof import("./lib/api")>("./lib/api");
  return {
    ...actual,
    createLocalApiClient,
  };
});

const snapshot: BootstrapSnapshot = {
  localDeviceName: "Local Stable",
  health: {
    status: "ok",
    discovery: "broadcast-ok",
    localAPIReady: true,
    agentPort: 19090,
  },
  peers: [],
  pairings: [],
  conversations: [],
  messages: [],
  transfers: [],
  eventSeq: 1,
};

afterEach(() => {
  createLocalApiClient.mockReset();
  vi.resetModules();
});

describe("AppShell default api", () => {
  it("creates the default local api client only once across re-renders", async () => {
    createLocalApiClient.mockReturnValue({
      bootstrap: vi.fn().mockResolvedValue(snapshot),
      startPairing: vi.fn(),
      confirmPairing: vi.fn(),
      sendText: vi.fn(),
      sendFile: vi.fn(),
      pickLocalFile: vi.fn(),
      sendAcceleratedFile: vi.fn(),
      listMessageHistory: vi.fn(),
      subscribeEvents: vi.fn(() => ({
        close: vi.fn(),
        reconnect: vi.fn(),
      })),
    });

    const { default: AppShell } = await import("./AppShell");
    render(<AppShell />);

    await Promise.resolve();
    await Promise.resolve();
    expect(createLocalApiClient).toHaveBeenCalledTimes(1);
  });
});
