import type { HealthSnapshot } from "../lib/types";

type HealthBannerProps = {
  health: HealthSnapshot;
  lastEventSeq: number;
};

export function HealthBanner({ health, lastEventSeq }: HealthBannerProps) {
  return (
    <section className={`ms-panel ms-health ms-health--${resolveHealthTone(health.status)}`}>
      <div className="ms-health__header">
        <div>
          <span className="ms-eyebrow">连接状态</span>
          <h2 className="ms-health__title">{resolveHealthHeadline(health.status)}</h2>
        </div>
        <div className="ms-health__chips">
          <span className="ms-chip ms-chip--soft">自动发现 {resolveDiscoveryLabel(health.discovery)}</span>
          <span className="ms-chip ms-chip--soft">代理端口 {health.agentPort ?? 19090}</span>
          <span className="ms-chip ms-chip--soft">事件序号 {lastEventSeq}</span>
        </div>
      </div>

      {health.issues?.length ? (
        <div className="ms-health__issues">
          {health.issues.map((issue) => (
            <p key={issue}>{issue}</p>
          ))}
        </div>
      ) : (
        <p className="ms-health__ok-copy">本地代理在线，设备发现与消息同步都已接入当前页面。</p>
      )}
    </section>
  );
}

function resolveHealthHeadline(status: string): string {
  if (status === "ok") {
    return "本机代理已就绪";
  }
  if (status === "degraded") {
    return "网络状态需要关注";
  }
  if (status === "error") {
    return "当前无法稳定传输";
  }
  return "正在同步局域网状态";
}

function resolveHealthTone(status: string): string {
  if (status === "ok") {
    return "ok";
  }
  if (status === "degraded") {
    return "warn";
  }
  if (status === "error") {
    return "error";
  }
  return "neutral";
}

function resolveDiscoveryLabel(discovery: string): string {
  if (discovery === "broadcast-ok") {
    return "已开启";
  }
  if (discovery === "broadcast-pending") {
    return "发现中";
  }
  if (discovery === "broadcast-error") {
    return "异常";
  }
  return discovery;
}
