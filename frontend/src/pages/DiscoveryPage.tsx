import { DeviceList } from "../components/DeviceList";
import { SettingsPage } from "./SettingsPage";
import type { PeerSummary } from "../lib/types";

type DiscoveryPageProps = {
  peers: PeerSummary[];
  selectedPeerId?: string;
  onSelect: (peer: PeerSummary) => void;
  localDeviceName: string;
  syncMode: string;
};

export function DiscoveryPage({
  peers,
  selectedPeerId,
  onSelect,
  localDeviceName,
  syncMode,
}: DiscoveryPageProps) {
  return (
    <aside className="ms-sidebar">
      <DeviceList peers={peers} selectedPeerId={selectedPeerId} onSelect={onSelect} />
      <SettingsPage localDeviceName={localDeviceName} syncMode={syncMode} />
    </aside>
  );
}
