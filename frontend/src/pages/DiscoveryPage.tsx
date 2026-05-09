import { DeviceList } from "../components/DeviceList";
import { SettingsPage } from "./SettingsPage";
import type { PeerSummary } from "../lib/types";

type DiscoveryPageProps = {
  peers: PeerSummary[];
  selectedPeerId?: string;
  onSelect: (peer: PeerSummary) => void;
  localDeviceName: string;
  syncMode: string;
  collapsed: boolean;
  onToggleCollapsed: () => void;
};

export function DiscoveryPage({
  peers,
  selectedPeerId,
  onSelect,
  localDeviceName,
  syncMode,
  collapsed,
  onToggleCollapsed,
}: DiscoveryPageProps) {
  return (
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
  );
}
