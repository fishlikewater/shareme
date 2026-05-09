import { useEffect, useState } from "react";

import { HealthBanner } from "./HealthBanner";
import { TransferStatusBanner } from "./TransferStatusBanner";
import type { HealthSnapshot, TransferSnapshot } from "../lib/types";

type WorkbenchStatusPanelProps = {
  health: HealthSnapshot;
  lastEventSeq: number;
  transfers: TransferSnapshot[];
};

export function WorkbenchStatusPanel({
  health,
  lastEventSeq,
  transfers,
}: WorkbenchStatusPanelProps) {
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
