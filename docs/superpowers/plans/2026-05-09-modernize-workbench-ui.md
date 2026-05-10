# Modernize Workbench UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 将 shareme 前端从展示型卡片首页改成现代清爽的工作台 UI，优先优化设备切换与设备状态判断。

**Architecture:** 保持现有 React + TypeScript 前端和 `LocalApi` 契约不变，只重排 `AppShell` 的布局组合，并让 `DeviceList` 支持可折叠 Dock。新增一个轻量 `WorkbenchStatusPanel` 作为健康状态与传输状态的视觉容器，不合并底层业务组件。

**Tech Stack:** React 18, TypeScript, Vite, Vitest, Testing Library, plain CSS in `frontend/src/styles.css`.

---

## Preconditions

- OpenSpec change: `openspec/changes/modernize-workbench-ui/`
- Design reference: `docs/superpowers/specs/2026-05-09-modernize-workbench-ui-design.md`
- Repo policy: do not create a git commit unless the user explicitly asks for it.
- Primary verification commands:
  - `cd frontend; npm test`
  - `cd frontend; npm run build`

## File Structure

- Modify `frontend/src/AppShell.tsx`
  - Owns `deviceDockCollapsed` UI state.
  - Replaces hero/stat/header stack with app bar + workbench grid.
  - Passes collapsed state into `DiscoveryPage`.
  - Renders `WorkbenchStatusPanel` in the status area.
- Modify `frontend/src/pages/DiscoveryPage.tsx`
  - Receives `collapsed` and `onToggleCollapsed`.
  - Wraps `DeviceList` as a device Dock.
  - Keeps `SettingsPage` available only in expanded/secondary area.
- Modify `frontend/src/components/DeviceList.tsx`
  - Adds collapsible Dock rendering.
  - Keeps `button` device rows and `aria-pressed`.
  - Adds status text that is not color-only.
- Modify `frontend/src/components/ChatPane.tsx`
  - Makes the header and empty states work inside the new main workspace.
  - Keeps send, pick file, big-file, and history behavior intact.
- Modify `frontend/src/components/PairCodeDialog.tsx`
  - Restyles as an in-workspace trust task block.
- Create `frontend/src/components/WorkbenchStatusPanel.tsx`
  - Owns status-panel visual shell and expand/collapse behavior.
  - Reuses `HealthBanner` and `TransferStatusBanner`.
- Modify `frontend/src/components/HealthBanner.tsx`
  - Make copy and heading suitable for status-panel placement.
- Modify `frontend/src/components/TransferStatusBanner.tsx`
  - Keep active-transfer semantics and progressbar accessibility.
- Modify `frontend/src/styles.css`
  - Replace hero/card-heavy layout with workbench grid, Dock, status panel, responsive rules, focus states, and reduced-motion rules.
- Modify tests:
  - `frontend/src/App.test.tsx`
  - `frontend/src/components/DeviceList.test.tsx`
  - `frontend/src/components/ChatPane.test.tsx`
  - `frontend/src/components/TransferStatusBanner.test.tsx`
  - Add `frontend/src/components/WorkbenchStatusPanel.test.tsx`

---

### Task 1: Lock Workbench Behavior With Failing Tests

**Files:**
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/components/DeviceList.test.tsx`
- Create: `frontend/src/components/WorkbenchStatusPanel.test.tsx`

- [x] **Step 1: Add App-level workbench layout assertions**

In the first `App` render test, change the expectations away from the hero copy and toward the workbench shell:

```tsx
expect(await screen.findByRole("banner", { name: "shareme 工作台" })).toBeInTheDocument();
expect(screen.getByText("本机设备")).toBeInTheDocument();
expect(screen.getByText("我的电脑")).toBeInTheDocument();
expect(screen.getByRole("navigation", { name: "设备 Dock" })).toBeInTheDocument();
expect(screen.getByRole("main", { name: "会话工作区" })).toBeInTheDocument();
expect(screen.queryByText("一页直传")).not.toBeInTheDocument();
```

If existing fixture text appears mojibake in the file, use the existing literal fixture values rather than re-encoding unrelated strings.

- [x] **Step 2: Add Dock collapse behavior test**

Append an App test:

```tsx
it("可以折叠和展开设备 Dock，且不改变当前选中设备", async () => {
  const api = new FakeApi(bootstrapSnapshot);
  render(<App api={api} />);

  expect(await screen.findByRole("navigation", { name: "设备 Dock" })).toBeInTheDocument();
  const selectedPeer = screen.getByRole("button", { name: /办公室副机/ });
  expect(selectedPeer).toHaveAttribute("aria-pressed", "true");

  fireEvent.click(screen.getByRole("button", { name: "收起设备 Dock" }));

  expect(screen.getByRole("navigation", { name: "设备 Dock" })).toHaveClass("is-collapsed");
  expect(screen.getByRole("button", { name: /办公室副机/ })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByRole("button", { name: "展开设备 Dock" })).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "展开设备 Dock" }));

  expect(screen.getByRole("navigation", { name: "设备 Dock" })).not.toHaveClass("is-collapsed");
});
```

- [x] **Step 3: Add DeviceList collapsed-state test**

In `DeviceList.test.tsx`, add:

```tsx
it("收起时仍提供设备短名、状态文本和选中语义", () => {
  const peer: PeerSummary = {
    deviceId: "peer-1",
    deviceName: "办公室副机",
    trusted: true,
    online: true,
    reachable: true,
    agentTcpPort: 19090,
  };

  render(<DeviceList peers={[peer]} selectedPeerId="peer-1" collapsed onSelect={vi.fn()} />);

  const deviceButton = screen.getByRole("button", { name: /办公室副机/ });
  expect(deviceButton).toHaveAttribute("aria-pressed", "true");
  expect(deviceButton).toHaveAccessibleName(expect.stringContaining("已配对"));
  expect(deviceButton).toHaveAccessibleName(expect.stringContaining("可发送"));
});
```

- [x] **Step 4: Add status-panel tests**

Create `frontend/src/components/WorkbenchStatusPanel.test.tsx`:

```tsx
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { WorkbenchStatusPanel } from "./WorkbenchStatusPanel";
import type { HealthSnapshot, TransferSnapshot } from "../lib/types";

const health: HealthSnapshot = {
  status: "ok",
  discovery: "broadcast-ok",
  localAPIReady: true,
  agentPort: 19090,
  issues: [],
};

const activeTransfer: TransferSnapshot = {
  transferId: "transfer-1",
  messageId: "msg-1",
  fileName: "demo.pdf",
  fileSize: 2_097_152,
  state: "sending",
  createdAt: "2026-05-09T10:00:00Z",
  direction: "outgoing",
  bytesTransferred: 1_048_576,
  progressPercent: 50,
  rateBytesPerSec: 512_000,
  etaSeconds: 4,
  active: true,
};

describe("WorkbenchStatusPanel", () => {
  it("正常且无传输时以摘要呈现", () => {
    render(<WorkbenchStatusPanel health={health} lastEventSeq={7} transfers={[]} />);

    const panel = screen.getByRole("complementary", { name: "运行状态" });
    expect(within(panel).getByText("本机服务已就绪")).toBeInTheDocument();
    expect(within(panel).queryByLabelText("传输横幅")).not.toBeInTheDocument();
  });

  it("有活跃传输时自动展示传输摘要", () => {
    render(<WorkbenchStatusPanel health={health} lastEventSeq={7} transfers={[activeTransfer]} />);

    expect(screen.getByLabelText("传输横幅")).toBeInTheDocument();
    expect(screen.getByText("demo.pdf")).toBeInTheDocument();
  });

  it("允许用户展开和收起详情", () => {
    render(<WorkbenchStatusPanel health={health} lastEventSeq={7} transfers={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "展开运行状态" }));
    expect(screen.getByRole("button", { name: "收起运行状态" })).toBeInTheDocument();
  });
});
```

- [x] **Step 5: Run focused tests and confirm failure**

Run:

```powershell
Push-Location frontend
npm test -- --run src/App.test.tsx src/components/DeviceList.test.tsx src/components/WorkbenchStatusPanel.test.tsx
Pop-Location
```

Expected: fails because `WorkbenchStatusPanel` does not exist and the new workbench roles/classes are not implemented.

---

### Task 2: Build AppShell Workbench Skeleton

**Files:**
- Modify: `frontend/src/AppShell.tsx`
- Modify: `frontend/src/pages/DiscoveryPage.tsx`
- Create: `frontend/src/components/WorkbenchStatusPanel.tsx`

- [x] **Step 1: Add Dock collapsed state in `AppShell`**

Near other UI state:

```tsx
const [deviceDockCollapsed, setDeviceDockCollapsed] = useState(false);
```

- [x] **Step 2: Replace hero/stat layout with app bar and workbench grid**

Replace the loaded-state JSX from `<section className="ms-hero">` through `<section className="ms-layout">` with this structure:

```tsx
<header className="ms-appbar" role="banner" aria-label="shareme 工作台">
  <div className="ms-appbar__identity">
    <span className="ms-appbar__mark" aria-hidden="true">MS</span>
    <div>
      <span className="ms-eyebrow">shareme</span>
      <strong className="ms-appbar__title">传输工作台</strong>
    </div>
  </div>
  <div className="ms-appbar__meta">
    <span className="ms-chip ms-chip--soft">本机设备</span>
    <strong>{snapshot.localDeviceName}</strong>
    <span className="ms-chip ms-chip--soft">已发现 {discoveredCount}</span>
    <span className="ms-chip ms-chip--soft">可发送 {readyCount}</span>
  </div>
</header>

{commandError ? (
  <section className="ms-command-error" role="alert">
    {commandError}
  </section>
) : null}

<section className="ms-workbench" aria-label="传输工作台">
  <DiscoveryPage
    peers={peers}
    selectedPeerId={selectedPeer?.deviceId}
    onSelect={handleSelectPeer}
    localDeviceName={snapshot.localDeviceName}
    syncMode="点对点即时传输"
    collapsed={deviceDockCollapsed}
    onToggleCollapsed={() => setDeviceDockCollapsed((current) => !current)}
  />

  <main className="ms-main-column" ref={mainColumnRef} aria-label="会话工作区">
    <ChatPane
      peer={selectedPeer}
      conversationId={selectedConversation?.conversationId}
      messages={selectedMessages}
      sendingText={busyState.sendingText}
      sendingFile={busyState.sendingFile}
      pickingLocalFile={busyState.pickingLocalFile}
      sendingAcceleratedFile={busyState.sendingAcceleratedFile}
      pickedLocalFile={pickedLocalFile}
      historyHasMore={selectedHistoryHasMore}
      historyLoading={selectedHistoryLoading}
      historyError={selectedHistoryError}
      onSendText={handleSendText}
      onSendFile={handleSendFile}
      onPickLocalFile={handlePickLocalFile}
      onSendAcceleratedFile={handleSendAcceleratedFile}
      onLoadOlderMessages={handleLoadOlderMessages}
    />
    <PairCodeDialog
      peer={selectedPeer}
      busy={busyState.startingPairing || busyState.confirmingPairing}
      onStartPairing={handleStartPairing}
      onConfirmPairing={handleConfirmPairing}
    />
  </main>

  <WorkbenchStatusPanel health={snapshot.health} lastEventSeq={snapshot.eventSeq ?? 0} transfers={activeTransfers} />
</section>
```

Remove the now-unused `trustedCount` and `pendingCount` constants if they are no longer referenced.

- [x] **Step 3: Import `WorkbenchStatusPanel`**

At the top of `AppShell.tsx`:

```tsx
import { WorkbenchStatusPanel } from "./components/WorkbenchStatusPanel";
```

- [x] **Step 4: Update `DiscoveryPage` props**

Change `DiscoveryPageProps`:

```tsx
type DiscoveryPageProps = {
  peers: PeerSummary[];
  selectedPeerId?: string;
  onSelect: (peer: PeerSummary) => void;
  localDeviceName: string;
  syncMode: string;
  collapsed: boolean;
  onToggleCollapsed: () => void;
};
```

Render:

```tsx
<aside className={`ms-device-dock${collapsed ? " is-collapsed" : ""}`}>
  <DeviceList
    peers={peers}
    selectedPeerId={selectedPeerId}
    collapsed={collapsed}
    onSelect={onSelect}
    onToggleCollapsed={onToggleCollapsed}
  />
  {!collapsed ? <SettingsPage localDeviceName={localDeviceName} syncMode={syncMode} /> : null}
</aside>
```

- [x] **Step 5: Create `WorkbenchStatusPanel` minimal implementation**

```tsx
import { useEffect, useState } from "react";

import { HealthBanner } from "./HealthBanner";
import { TransferStatusBanner } from "./TransferStatusBanner";
import type { HealthSnapshot, TransferSnapshot } from "../lib/types";

type WorkbenchStatusPanelProps = {
  health: HealthSnapshot;
  lastEventSeq: number;
  transfers: TransferSnapshot[];
};

export function WorkbenchStatusPanel({ health, lastEventSeq, transfers }: WorkbenchStatusPanelProps) {
  const activeTransfers = transfers.filter((transfer) => transfer.active);
  const requiresAttention = health.status !== "ok" || activeTransfers.length > 0;
  const [expanded, setExpanded] = useState(requiresAttention);

  useEffect(() => {
    if (requiresAttention) {
      setExpanded(true);
    }
  }, [requiresAttention]);

  return (
    <aside className={`ms-status-panel${expanded ? " is-expanded" : ""}`} aria-label="运行状态">
      <div className="ms-status-panel__summary">
        <div>
          <span className="ms-eyebrow">运行状态</span>
          <strong>{resolveStatusSummary(health.status, activeTransfers.length)}</strong>
        </div>
        <button
          className="ms-icon-button"
          type="button"
          aria-label={expanded ? "收起运行状态" : "展开运行状态"}
          onClick={() => setExpanded((current) => !current)}
        >
          {expanded ? "收起" : "展开"}
        </button>
      </div>
      {expanded ? (
        <div className="ms-status-panel__details">
          <HealthBanner health={health} lastEventSeq={lastEventSeq} />
          <TransferStatusBanner transfers={activeTransfers} />
        </div>
      ) : null}
    </aside>
  );
}

function resolveStatusSummary(status: string, activeTransferCount: number): string {
  if (activeTransferCount > 0) {
    return `${activeTransferCount} 个传输进行中`;
  }
  if (status === "ok") {
    return "本机服务已就绪";
  }
  if (status === "degraded") {
    return "网络状态需要关注";
  }
  if (status === "error") {
    return "传输服务异常";
  }
  return "正在同步局域网状态";
}
```

- [x] **Step 6: Run focused tests**

Run the same focused command from Task 1. Expected: `WorkbenchStatusPanel` tests pass; App and DeviceList tests still fail until later tasks.

---

### Task 3: Implement Foldable Device Dock

**Files:**
- Modify: `frontend/src/components/DeviceList.tsx`
- Modify: `frontend/src/components/DeviceList.test.tsx`

- [x] **Step 1: Extend `DeviceListProps`**

```tsx
type DeviceListProps = {
  peers: PeerSummary[];
  selectedPeerId?: string;
  collapsed?: boolean;
  onSelect: (peer: PeerSummary) => void;
  onToggleCollapsed?: () => void;
};
```

- [x] **Step 2: Add Dock header and collapse button**

Use a `nav` landmark:

```tsx
return (
  <nav className={`ms-device-panel${collapsed ? " is-collapsed" : ""}`} aria-label="设备 Dock">
    <div className="ms-device-dock__header">
      {!collapsed ? (
        <div>
          <span className="ms-eyebrow">设备</span>
          <h2 className="ms-panel-title">已发现 {peers.length} 台</h2>
          <p className="ms-panel-copy">选择设备后即可查看会话与发送入口。</p>
        </div>
      ) : (
        <span className="ms-device-dock__compact-title">设备</span>
      )}
      {onToggleCollapsed ? (
        <button
          className="ms-icon-button"
          type="button"
          aria-label={collapsed ? "展开设备 Dock" : "收起设备 Dock"}
          onClick={onToggleCollapsed}
        >
          {collapsed ? "展" : "收"}
        </button>
      ) : null}
    </div>
    {/* device list continues here */}
  </nav>
);
```

- [x] **Step 3: Make each device button carry complete accessible state**

Inside `peers.map`, compute:

```tsx
const stateLabel = resolveStateLabel(peer);
const shortName = buildShortName(peer.deviceName);
const pressed = peer.deviceId === selectedPeerId;
const accessibleName = `${peer.deviceName}，${stateLabel}`;
```

Set:

```tsx
<button
  aria-label={accessibleName}
  aria-pressed={pressed}
  key={peer.deviceId}
  className={`ms-device-card${pressed ? " is-active" : ""}`}
  onClick={() => onSelect(peer)}
  type="button"
>
  <div className="ms-device-card__header">
    <div className="ms-device-card__identity">
      <span className="ms-device-card__avatar" aria-hidden="true">{shortName}</span>
      {!collapsed ? (
        <div>
          <span className="ms-device-card__name">{peer.deviceName}</span>
          <span className="ms-device-card__meta">{describePeer(peer)}</span>
        </div>
      ) : null}
    </div>
    <span className={`ms-status-dot ${resolveStatusClass(peer)}`} aria-hidden="true" />
  </div>

  {!collapsed ? (
    <>
      <div className="ms-badge-row">
        <span className={`ms-badge ${peer.trusted ? "ms-badge--ok" : "ms-badge--warn"}`}>
          {peer.trusted ? "已配对" : "待配对"}
        </span>
        <span className={`ms-badge ${peer.reachable ? "ms-badge--neutral" : "ms-badge--ghost"}`}>
          {peer.reachable ? "可发送" : "不可达"}
        </span>
      </div>
      <p className="ms-device-card__preview">{peer.lastMessagePreview ?? resolvePreview(peer)}</p>
    </>
  ) : (
    <span className="ms-device-card__compact-state">{stateLabel}</span>
  )}
</button>
```

- [x] **Step 4: Add helpers**

```tsx
function resolveStateLabel(peer: PeerSummary): string {
  if (peer.trusted && peer.reachable) {
    return "已配对，可发送";
  }
  if (!peer.trusted && peer.reachable) {
    return "待配对";
  }
  if (peer.trusted && !peer.reachable) {
    return "已配对，不可达";
  }
  return "不可达";
}

function buildShortName(deviceName: string): string {
  const compact = deviceName.trim();
  if (compact.length <= 2) {
    return compact || "?";
  }
  return compact.slice(0, 2);
}
```

- [x] **Step 5: Run DeviceList tests**

Run:

```powershell
Push-Location frontend
npm test -- --run src/components/DeviceList.test.tsx
Pop-Location
```

Expected: pass after updating old assertions to match the new reachable labels.

---

### Task 4: Move Conversation Work Into Main Workspace

**Files:**
- Modify: `frontend/src/components/ChatPane.tsx`
- Modify: `frontend/src/components/PairCodeDialog.tsx`
- Modify: `frontend/src/components/ChatPane.test.tsx`
- Modify: `frontend/src/App.test.tsx`

- [x] **Step 1: Compact `ChatPane` header copy**

Keep existing props. Change the top section to use current-device context:

```tsx
<section className="ms-panel ms-chat-panel" aria-label={peer ? `${peer.deviceName} 会话` : "会话工作区"}>
  <div className="ms-chat-header">
    <div>
      <span className="ms-eyebrow">当前会话</span>
      <h2 className="ms-chat-title">{peer ? peer.deviceName : "选择设备开始传输"}</h2>
      <p className="ms-chat-copy">{resolveChatCopy(peer)}</p>
    </div>
    {peer ? (
      <div className="ms-badge-row" aria-label="当前设备状态">
        {/* existing badges, with readable labels */}
      </div>
    ) : null}
  </div>
```

- [x] **Step 2: Keep send controls behavior unchanged**

Do not change these function calls:

```tsx
await onSendText(body);
await onSendFile();
await onPickLocalFile();
await onSendAcceleratedFile();
await onLoadOlderMessages();
```

Only change layout wrappers and copy.

- [x] **Step 3: Restyle PairCodeDialog as task block**

Change the root element:

```tsx
<section className="ms-panel ms-trust-task" aria-label="信任建立">
```

Keep button labels and callbacks:

```tsx
<button className="ms-button ms-button--primary" disabled={busy} onClick={onStartPairing} type="button">
  开始配对
</button>
```

```tsx
<button
  className="ms-button ms-button--secondary"
  disabled={busy}
  onClick={() => onConfirmPairing(peer.pairing!.pairingId)}
  type="button"
>
  确认配对
</button>
```

- [x] **Step 4: Update ChatPane test to assert behavior, not old layout**

Keep:

```tsx
fireEvent.click(screen.getByRole("button", { name: "选择文件" }));
expect(onSendFile).toHaveBeenCalledTimes(1);
expect(screen.queryByTestId("file-input")).not.toBeInTheDocument();
```

Add:

```tsx
expect(screen.getByRole("heading", { name: trustedPeer.deviceName })).toBeInTheDocument();
expect(screen.getByLabelText("当前设备状态")).toBeInTheDocument();
```

- [x] **Step 5: Run Chat/App focused tests**

Run:

```powershell
Push-Location frontend
npm test -- --run src/components/ChatPane.test.tsx src/App.test.tsx
Pop-Location
```

Expected: pass after updating old hero/status assertions.

---

### Task 5: Adapt Status Components For the Status Panel

**Files:**
- Modify: `frontend/src/components/HealthBanner.tsx`
- Modify: `frontend/src/components/TransferStatusBanner.tsx`
- Modify: `frontend/src/components/TransferStatusBanner.test.tsx`
- Modify: `frontend/src/components/WorkbenchStatusPanel.test.tsx`

- [x] **Step 1: Keep HealthBanner semantic content but reduce heading level**

Change the root to avoid nested heavy panel visuals:

```tsx
<section className={`ms-health ms-health--${resolveHealthTone(health.status)}`} aria-label="连接状态">
```

Use:

```tsx
<h3 className="ms-health__title">{resolveHealthHeadline(health.status)}</h3>
```

- [x] **Step 2: Keep TransferStatusBanner null behavior**

Do not change:

```tsx
if (activeTransfers.length === 0) {
  return null;
}
```

Keep `aria-label="传输横幅"` and progressbar attributes so existing tests remain meaningful.

- [x] **Step 3: Rename lead copy if needed, but preserve testable transfer facts**

The following facts must remain visible:

```tsx
transfer.fileName
formatDirectionLabel(transfer.direction)
displayPercent
ratio
rateLabel
etaLabel
stateLabel
```

- [x] **Step 4: Run status tests**

Run:

```powershell
Push-Location frontend
npm test -- --run src/components/TransferStatusBanner.test.tsx src/components/WorkbenchStatusPanel.test.tsx
Pop-Location
```

Expected: pass.

---

### Task 6: Rewrite CSS To Modern Clean Workbench

**Files:**
- Modify: `frontend/src/styles.css`

- [x] **Step 1: Replace global design tokens**

At `:root`, use this token set as the base:

```css
:root {
  color-scheme: light;
  --ms-bg: #f5f7f8;
  --ms-surface: #ffffff;
  --ms-surface-muted: #eef3f2;
  --ms-border: #d8e2df;
  --ms-border-strong: #b9cbc6;
  --ms-shadow: 0 14px 34px rgba(15, 35, 40, 0.08);
  --ms-text: #142522;
  --ms-muted: #60716d;
  --ms-muted-strong: #314540;
  --ms-teal: #0f766e;
  --ms-teal-soft: #e3f4f1;
  --ms-orange: #f97316;
  --ms-ok: #16815f;
  --ms-warn: #b96015;
  --ms-danger: #b42318;
  --ms-display: "Segoe UI Variable Display", "Microsoft YaHei", sans-serif;
  --ms-body: "Microsoft YaHei", "PingFang SC", "Noto Sans SC", sans-serif;
}
```

- [x] **Step 2: Add app shell and workbench grid**

```css
.ms-app {
  min-height: 100vh;
  padding: clamp(12px, 2vw, 24px);
  background: var(--ms-bg);
}

.ms-shell {
  width: min(1500px, 100%);
  margin: 0 auto;
  display: grid;
  gap: 12px;
}

.ms-appbar {
  min-height: 64px;
  border: 1px solid var(--ms-border);
  border-radius: 14px;
  background: var(--ms-surface);
  box-shadow: var(--ms-shadow);
  padding: 12px 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.ms-workbench {
  display: grid;
  grid-template-columns: 320px minmax(0, 1fr) minmax(260px, 340px);
  gap: 12px;
  align-items: start;
}

.ms-device-dock.is-collapsed {
  width: 88px;
}
```

- [x] **Step 3: Normalize panels and controls**

Use 8-12px radius for operational surfaces:

```css
.ms-panel,
.ms-device-panel,
.ms-status-panel,
.ms-chat-panel {
  border: 1px solid var(--ms-border);
  border-radius: 12px;
  background: var(--ms-surface);
  box-shadow: var(--ms-shadow);
}

.ms-button,
.ms-icon-button,
.ms-device-card {
  cursor: pointer;
}

.ms-button:focus-visible,
.ms-icon-button:focus-visible,
.ms-device-card:focus-visible,
.ms-textarea:focus-visible {
  outline: 3px solid rgba(249, 115, 22, 0.35);
  outline-offset: 2px;
}
```

- [x] **Step 4: Add responsive behavior**

```css
@media (max-width: 1180px) {
  .ms-workbench {
    grid-template-columns: 88px minmax(0, 1fr);
  }

  .ms-status-panel {
    grid-column: 1 / -1;
  }
}

@media (max-width: 760px) {
  .ms-app {
    padding: 8px;
  }

  .ms-appbar {
    align-items: flex-start;
    flex-direction: column;
  }

  .ms-workbench {
    grid-template-columns: 1fr;
  }

  .ms-device-dock,
  .ms-device-dock.is-collapsed {
    width: 100%;
  }

  .ms-device-list {
    display: flex;
    overflow-x: auto;
    padding-bottom: 4px;
  }

  .ms-device-card {
    min-width: 168px;
  }
}
```

- [x] **Step 5: Respect reduced motion**

```css
@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
  }
}
```

- [x] **Step 6: Run build**

Run:

```powershell
Push-Location frontend
npm run build
Pop-Location
```

Expected: TypeScript and Vite build pass.

---

### Task 7: Full Verification And Browser QA

**Files:**
- No planned source changes unless verification finds defects.

- [x] **Step 1: Run full frontend tests**

```powershell
Push-Location frontend
npm test
Pop-Location
```

Expected: all Vitest suites pass.

- [x] **Step 2: Run frontend build**

```powershell
Push-Location frontend
npm run build
Pop-Location
```

Expected: TypeScript build and Vite build pass.

- [x] **Step 3: Open local UI in browser**

Start Vite if needed:

```powershell
Push-Location frontend
npm run dev -- --host 127.0.0.1
Pop-Location
```

Open `http://127.0.0.1:<port>/` in the in-app browser.

- [x] **Step 4: Check responsive widths**

Use browser viewport checks at:

- 375px
- 768px
- 1024px
- 1440px

Verify:

- no page-level horizontal scroll
- Dock remains usable
- selected device state is visible
- send buttons do not overflow
- status panel does not hide the composer
- focus ring visible on Dock toggle, device buttons, composer, and send buttons

- [x] **Step 5: Update OpenSpec task status only after implementation passes**

When implementation and verification complete, update `openspec/changes/modernize-workbench-ui/tasks.md` checkboxes to `[x]`.

- [x] **Step 6: Handoff**

Report:

- files changed
- verification commands and results
- any visual QA notes
- no commit created unless the user explicitly requested one
