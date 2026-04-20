import { afterEach, describe, expect, it, vi } from "vitest";

const mockApi = {
  bootstrap: vi.fn(),
  startPairing: vi.fn(),
  confirmPairing: vi.fn(),
  sendText: vi.fn(),
  sendFile: vi.fn(),
  pickLocalFile: vi.fn(),
  sendAcceleratedFile: vi.fn(),
  listMessageHistory: vi.fn(),
  subscribeEvents: vi.fn(() => ({ close: vi.fn(), reconnect: vi.fn() })),
};

const createDesktopApiClient = vi.fn(() => mockApi);
const createLocalhostApiClient = vi.fn(() => mockApi);
const hasDesktopApiBindings = vi.fn();

vi.mock("./desktop-api", () => ({
  createDesktopApiClient,
  hasDesktopApiBindings,
}));

vi.mock("./localhost-api", () => ({
  createLocalhostApiClient,
}));

describe("createDefaultLocalApi", () => {
  const originalWindow = globalThis.window;

  afterEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: originalWindow,
    });
  });

  it("桌面 bindings 可用时优先返回 desktop client", async () => {
    hasDesktopApiBindings.mockReturnValue(true);

    const { createDefaultLocalApi } = await import("./api");
    const api = createDefaultLocalApi();

    expect(api).toBe(mockApi);
    expect(createDesktopApiClient).toHaveBeenCalledTimes(1);
    expect(createLocalhostApiClient).not.toHaveBeenCalled();
  });

  it.each(["localhost", "127.0.0.1", "[::1]"])(
    "桌面 bindings 不可用且页面位于 %s 时切换到 localhost client",
    async (hostname) => {
      hasDesktopApiBindings.mockReturnValue(false);
      Object.defineProperty(globalThis, "window", {
        configurable: true,
        value: {
          location: {
            hostname,
            origin: `http://${hostname}:52350`,
          },
        },
      });

      const { createDefaultLocalApi } = await import("./api");
      const api = createDefaultLocalApi();

      expect(api).toBe(mockApi);
      expect(createDesktopApiClient).not.toHaveBeenCalled();
      expect(createLocalhostApiClient).toHaveBeenCalledWith({
        origin: `http://${hostname}:52350`,
      });
    },
  );

  it("非 loopback 浏览器且没有桌面 bindings 时抛错", async () => {
    hasDesktopApiBindings.mockReturnValue(false);
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: {
        location: {
          hostname: "example.com",
          origin: "https://example.com",
        },
      },
    });

    const { createDefaultLocalApi } = await import("./api");

    expect(() => createDefaultLocalApi()).toThrowError("local api bindings not available");
    expect(createDesktopApiClient).not.toHaveBeenCalled();
    expect(createLocalhostApiClient).not.toHaveBeenCalled();
  });
});
