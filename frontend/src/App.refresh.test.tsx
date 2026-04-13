import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import App from "./App";
import type { LocalApi } from "./lib/api";
import type { BootstrapSnapshot } from "./lib/types";

describe("App refresh", () => {
  it("periodically reloads bootstrap so non-event state changes are eventually reflected", async () => {
    vi.useFakeTimers();

    const first: BootstrapSnapshot = {
      localDeviceName: "Local One",
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
    const second: BootstrapSnapshot = {
      ...first,
      localDeviceName: "Local Two",
      eventSeq: 2,
    };

    const api: LocalApi = {
      bootstrap: vi.fn().mockResolvedValueOnce(first).mockResolvedValue(second),
      startPairing: vi.fn(),
      confirmPairing: vi.fn(),
      sendText: vi.fn(),
      sendFile: vi.fn(),
      subscribeEvents: vi.fn(() => ({
        close: vi.fn(),
        reconnect: vi.fn(),
      })),
    };

    render(<App api={api} />);

    await Promise.resolve();
    await Promise.resolve();
    expect(screen.getAllByText("Local One").length).toBeGreaterThan(0);

    await vi.advanceTimersByTimeAsync(3000);

    await Promise.resolve();
    expect(screen.getAllByText("Local Two").length).toBeGreaterThan(0);
  });
});
