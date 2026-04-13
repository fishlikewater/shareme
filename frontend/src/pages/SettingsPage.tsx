type SettingsPageProps = {
  localDeviceName: string;
  syncMode: string;
};

export function SettingsPage({ localDeviceName, syncMode }: SettingsPageProps) {
  return (
    <section className="ms-panel ms-settings-card">
      <span className="ms-eyebrow">本机信息</span>
      <h2 className="ms-settings-card__title">{localDeviceName}</h2>
      <dl className="ms-settings-list">
        <div>
          <dt>传输方式</dt>
          <dd>{syncMode}</dd>
        </div>
        <div>
          <dt>配对策略</dt>
          <dd>首次建立信任后即可重复使用</dd>
        </div>
      </dl>
      <p className="ms-settings-card__tip">适合办公网、家庭网和临时协作场景，不需要账号体系。</p>
    </section>
  );
}
