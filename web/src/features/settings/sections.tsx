import {
  Activity,
  AudioLines,
  BarChart3,
  CalendarClock,
  Webhook,
  Cpu,
  Database,
  Download,
  Globe,
  HeartPulse,
  Library,
  MonitorSmartphone,
  Network,
  Popcorn,
  ScrollText,
  Server,
  Users,
  Inbox,
  Tv,
} from "lucide-react";
import type { ComponentType } from "react";
import { CinemaSettings } from "./CinemaSettings";
import { DashboardSettings } from "./DashboardSettings";
import { MusicSettings } from "./MusicSettings";
import { LibrariesSettings } from "./LibrariesSettings";
import { LogsSettings } from "./LogsSettings";
import { PlexImportSettings } from "./PlexImportSettings";
import {
  GeneralSettings,
  MetadataSettings,
  NetworkSettings,
  RemoteAccessSettings,
} from "./ServerSections";
import { StatsView } from "./StatsView";
import { LiveTvSettings } from "./LiveTvSettings";
import { LibraryHealthSettings } from "./LibraryHealth";
import { RequestsSettings } from "./RequestsSettings";
import { WebhooksSettings } from "./WebhooksSettings";
import { ScheduledTasksSettings } from "./TasksSettings";
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
  /** Extra words for settings search: the settings inside the section. */
  keywords?: string;
};

// Structure follows docs/01-requirements.md §7a.
export const settingsGroups: { label: string; sections: SettingsSection[] }[] =
  [
    {
      label: "Server",
      sections: [
        {
          id: "general",
          label: "General",
          icon: Server,
          component: GeneralSettings,
          keywords: "server name language restart reboot",
          description: "Server identity and defaults.",
        },
        {
          id: "libraries",
          label: "Libraries",
          icon: Library,
          component: LibrariesSettings,
          keywords:
            "folders paths scan add library ignore patterns watcher file watching empty trash refresh metadata trickplay seek previews thumbnails",
          description:
            "Add and manage the folders Marquee organises, and how they are scanned.",
        },
        {
          id: "network",
          label: "Network",
          icon: Network,
          component: NetworkSettings,
          keywords: "lan subnets local port discovery bonjour address",
          description:
            "Which devices count as local, and how local apps find this server.",
        },
        {
          id: "remote-access",
          label: "Remote Access",
          icon: Globe,
          component: RemoteAccessSettings,
          keywords:
            "tailscale remote wan upload speed bandwidth limit internet",
          description:
            "Streaming to devices away from home over Tailscale, and how bandwidth is shared.",
        },
        {
          id: "transcoder",
          label: "Transcoder",
          icon: Cpu,
          component: TranscoderSettings,
          keywords:
            "nvenc qsv quick sync gpu hardware encoder hevc tone mapping quality ladder preset max transcodes throttle",
          description:
            "Hardware encoders, quality and the automatic remote quality ladder.",
        },
        {
          id: "metadata",
          label: "Metadata",
          icon: Database,
          component: MetadataSettings,
          keywords:
            "tmdb omdb api key ratings imdb rotten tomatoes artwork posters deezer",
          description: "Online metadata providers and their API keys.",
        },
        {
          id: "music",
          label: "Music",
          icon: AudioLines,
          component: MusicSettings,
          keywords:
            "sonic analysis radio sage mixes lyrics lrclib loudness replaygain volume levelling gpu",
          description: "Sonic analysis, lyrics and volume levelling.",
        },
        {
          id: "cinema",
          label: "Cinema Trailers",
          icon: Popcorn,
          component: CinemaSettings,
          keywords: "trailers preroll pre-roll intro cinema before movie extras",
          description:
            "Trailers and a pre-roll video before movies, like at the cinema.",
        },
        {
          id: "scheduled-tasks",
          label: "Scheduled Tasks",
          icon: CalendarClock,
          component: ScheduledTasksSettings,
          keywords:
            "maintenance window backup restore database optimize tasks run now retention",
          description:
            "Maintenance window, background tasks and database backups.",
        },
        {
          id: "live-tv",
          label: "Live TV",
          icon: Tv,
          component: LiveTvSettings,
          keywords:
            "live tv iptv m3u xmltv epg guide dispatcharr pluto channels tuner",
          description:
            "Channels and the guide from Dispatcharr or an M3U playlist.",
        },
        {
          id: "requests",
          label: "Requests",
          icon: Inbox,
          component: RequestsSettings,
          keywords:
            "seerr overseerr jellyseerr requests approve decline radarr sonarr discover bazarr subtitles integrations",
          description:
            "Connect Seerr and Bazarr, and approve what people request.",
        },
        {
          id: "webhooks",
          label: "Webhooks",
          icon: Webhook,
          component: WebhooksSettings,
          keywords:
            "webhooks notifications home assistant discord integrations events play stop new",
          description:
            "Tell other services when something plays or new titles arrive.",
        },
        {
          id: "plex-import",
          label: "Plex Import",
          icon: Download,
          component: PlexImportSettings,
          keywords: "plex import watch history migrate accounts path mapping",
          description:
            "Bring over watch history, playlists and customisations from Plex.",
        },
      ],
    },
    {
      label: "Manage",
      sections: [
        {
          id: "library-health",
          label: "Library Health",
          icon: HeartPulse,
          component: LibraryHealthSettings,
          keywords:
            "health duplicates unmatched missing files unavailable unplayable upgrades playback errors artwork posters subtitles bazarr ignore problems",
          description:
            "Duplicates, unmatched titles, missing files and other things worth fixing.",
        },
        {
          id: "users",
          label: "Users",
          icon: Users,
          component: UsersSettings,
          keywords:
            "accounts managed profiles pin password restrictions content rating sign-in profile pictures",
          description: "Accounts, managed profiles, PINs and library access.",
        },
        {
          id: "devices",
          label: "Devices",
          icon: MonitorSmartphone,
          component: DevicesSettings,
          keywords: "sign out revoke sessions apps browsers",
          description: "Signed-in apps and browsers. Sign devices out here.",
        },
      ],
    },
    {
      label: "Activity",
      sections: [
        {
          id: "dashboard",
          label: "Dashboard",
          icon: Activity,
          component: DashboardSettings,
          keywords: "now playing streams bandwidth graph history stop",
          description: "Now playing, bandwidth, transcodes and recent plays.",
        },
        {
          id: "statistics",
          label: "Statistics",
          icon: BarChart3,
          component: StatisticsSettings,
          keywords:
            "stats plays history most watched top movies shows artists users tautulli year in music",
          description:
            "What's been played, by whom, on which apps, and how it was delivered.",
        },
        {
          id: "logs",
          label: "Logs",
          icon: ScrollText,
          component: LogsSettings,
          keywords: "errors troubleshooting debug",
          description: "What the server has been doing, for troubleshooting.",
        },
      ],
    },
  ];

function StatisticsSettings() {
  return <StatsView admin />;
}

export function findSection(id: string) {
  for (const g of settingsGroups) {
    const s = g.sections.find((x) => x.id === id);
    if (s) return s;
  }
  return undefined;
}
