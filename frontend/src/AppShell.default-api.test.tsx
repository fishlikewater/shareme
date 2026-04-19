import { render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { BootstrapSnapshot } from "./lib/types";

const createDefaultLocalApi = vi.fn();
const notifyDesktopUiReady = vi.fn().mockResolvedValue(undefined);

vi.mock("./lib/api", async () => {
  const actual = await vi.importActual<typeof import("./lib/api")>("./lib/api");
  return {
    ...actual,
    createDefaultLocalApi,
  };
});

vi.mock("./lib/desktop-api", async () => {
  const actual = await vi.importActual<typeof import("./lib/desktop-api")>("./lib/desktop-api");
  return {
    ...actual,
    notifyDesktopUiReady,
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
  createDefaultLocalApi.mockReset();
  notifyDesktopUiReady.mockClear();
  vi.resetModules();
});

describe("AppShell default api", () => {
  it("creates the default local api only once across re-renders", async () => {
    createDefaultLocalApi.mockReturnValue({
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
    expect(createDefaultLocalApi).toHaveBeenCalledTimes(1);
  });

  it("notifies desktop ui ready only after the main snapshot is loaded", async () => {
    let resolveBootstrap: ((value: BootstrapSnapshot) => void) | undefined;
    const bootstrap = vi.fn().mockImplementation(
      () =>
        new Promise<BootstrapSnapshot>((resolve) => {
          resolveBootstrap = resolve;
        }),
    );

    createDefaultLocalApi.mockReturnValue({
      bootstrap,
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

    expect(notifyDesktopUiReady).not.toHaveBeenCalled();

    resolveBootstrap?.(snapshot);

    await waitFor(() => {
      expect(notifyDesktopUiReady).toHaveBeenCalledTimes(1);
    });
  });

  it("does not notify desktop ui ready when bootstrap falls into the error screen", async () => {
    createDefaultLocalApi.mockReturnValue({
      bootstrap: vi.fn().mockRejectedValue(new Error("bootstrap failed")),
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

    await waitFor(() => {
      expect(notifyDesktopUiReady).not.toHaveBeenCalled();
    });
  });
});
