import { Activity, CalendarClock, Cpu, Database, Download, Globe, Library, MonitorSmartphone, Network, ScrollText, Server, Users } from "lucide-react";
import type { ComponentType } from "react";
import { LibrariesSettings } from "./LibrariesSettings";
import { LogsSettings } from "./LogsSettings";
import { PlexImportSettings } from "./PlexImportSettings";
import { GeneralSettings, MetadataSettings, NetworkSettings, RemoteAccessSettings, TaskSettings } from "./ServerSections";
import { TranscoderSettings } from "./TranscoderSettings";
import { DevicesSettings } from "../users/DevicesSettings";
import { UsersSettings } from "../users/UsersSettings";

export type SettingsSection = {
  id: string;
  label: string;
  description: string;
  icon: ComponentType<{ className?: string }>;
  component?: ComponentType;
  /** Milestone that delivers a section not yet built. */
  milestone?: string;
};

// Structure follows docs/01-requirements.md §7a.
export const settingsGroups: { label: string; sections: SettingsSection[] }[] = [
  {
    label: "Server",
    sections: [
      { id: "general", label: "General", icon: Server, component: GeneralSettings, description: "Server identity and defaults." },
      { id: "libraries", label: "Libraries", icon: Library, component: LibrariesSettings, description: "Add and manage the folders Marquee organises, and how they are scanned." },
      { id: "network", label: "Network", icon: Network, component: NetworkSettings, description: "Which devices count as local, and how local apps find this server." },
      { id: "remote-access", label: "Remote Access", icon: Globe, component: RemoteAccessSettings, description: "Streaming to devices away from home over Tailscale, and how bandwidth is shared." },
      { id: "transcoder", label: "Transcoder", icon: Cpu, component: TranscoderSettings, description: "Hardware encoders, quality and the automatic remote quality ladder." },
      { id: "metadata", label: "Metadata", icon: Database, component: MetadataSettings, description: "Online metadata providers and their API keys." },
      { id: "scheduled-tasks", label: "Scheduled Tasks", icon: CalendarClock, component: TaskSettings, description: "Maintenance window and database backups." },
      { id: "plex-import", label: "Plex Import", icon: Download, component: PlexImportSettings, description: "Bring over watch history, playlists and customisations from Plex." },
    ],
  },
  {
    label: "Manage",
    sections: [
      { id: "users", label: "Users", icon: Users, component: UsersSettings, description: "Accounts, managed profiles, PINs and library access." },
      { id: "devices", label: "Devices", icon: MonitorSmartphone, component: DevicesSettings, description: "Signed-in apps and browsers. Sign devices out here." },
    ],
  },
  {
    label: "Activity",
    sections: [
      { id: "dashboard", label: "Dashboard", icon: Activity, milestone: "M3", description: "Now playing, bandwidth and active transcodes." },
      { id: "logs", label: "Logs", icon: ScrollText, component: LogsSettings, description: "What the server has been doing, for troubleshooting." },
    ],
  },
];

export function findSection(id: string) {
  for (const g of settingsGroups) {
    const s = g.sections.find((x) => x.id === id);
    if (s) return s;
  }
  return undefined;
}
