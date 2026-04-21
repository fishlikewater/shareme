import { afterEach, describe, expect, it, vi } from "vitest";

const createDesktopApiClient = vi.fn();
const hasDesktopApiBindings = vi.fn();
const createLocalhostApiClient = vi.fn();

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
    createDesktopApiClient.mockReset();
    createLocalhostApiClient.mockReset();
    hasDesktopApiBindings.mockReset();

    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: originalWindow,
    });
  });

  it("prefers desktop bindings when they are available", async () => {
    hasDesktopApiBindings.mockReturnValue(true);
    const desktopApi = { host: "desktop" };
    createDesktopApiClient.mockReturnValue(desktopApi);

    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: {
        location: new URL("http://localhost:4173/app"),
      },
    });

    const { createDefaultLocalApi } = await import("./api");

    expect(createDefaultLocalApi()).toBe(desktopApi);
    expect(createDesktopApiClient).toHaveBeenCalledTimes(1);
    expect(createLocalhostApiClient).not.toHaveBeenCalled();
  });

  it.each([
    "http://localhost:4173/app",
    "http://127.0.0.1:4173/app",
    "http://[::1]:4173/app",
  ])("falls back to the localhost client on loopback browser hosts: %s", async (url) => {
    hasDesktopApiBindings.mockReturnValue(false);
    const localhostApi = { host: "localhost" };
    createLocalhostApiClient.mockReturnValue(localhostApi);

    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: {
        location: new URL(url),
      },
    });

    const { createDefaultLocalApi } = await import("./api");

    expect(createDefaultLocalApi()).toBe(localhostApi);
    expect(createLocalhostApiClient).toHaveBeenCalledTimes(1);
    expect(createDesktopApiClient).not.toHaveBeenCalled();
  });

  it("throws on non-loopback browsers when desktop bindings are unavailable", async () => {
    hasDesktopApiBindings.mockReturnValue(false);

    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: {
        location: new URL("https://example.com/app"),
      },
    });

    const { createDefaultLocalApi } = await import("./api");

    expect(() => createDefaultLocalApi()).toThrow(/loopback/i);
    expect(createDesktopApiClient).not.toHaveBeenCalled();
    expect(createLocalhostApiClient).not.toHaveBeenCalled();
  });
});
