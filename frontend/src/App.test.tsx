import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import App from "./App";

describe("App", () => {
  it("renders product title", () => {
    render(<App />);
    expect(screen.getByText("Message Share")).toBeInTheDocument();
    expect(screen.getByText("本机代理未连接")).toBeInTheDocument();
  });
});
