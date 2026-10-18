import {
  Activity,
  AudioLines,
  Bell,
  Sparkles,
  BarChart3,
  CalendarClock,
  Webhook,
  Cpu,
  Database,
  Download,
  Film,
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
import { AiSettings } from "./AiSettings";
import { CinemaSettings } from "./CinemaSettings";
import { DashboardSettings } from "./DashboardSettings";
import { MusicSettings } from "./MusicSettings";
import { NotificationsSettings } from "./NotificationsSettings";
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
import { VodSettings } from "./VodSettings";
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
          keywords: "server name language restart reboot rating prompt rate stars ask",
          description: "Server identity and defaults.",
        },
        {
          id: "libraries",
          label: "Libraries",
          icon: Library,
          component: LibrariesSettings,
          keywords:
            "folders paths scan add library ignore patterns watcher file watching empty trash refresh metadata trickplay seek previews thumbnails delete media files duplicates",
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
            "soundprint analysis sound journey radio sage mixes lyrics lrclib loudness replaygain volume levelling gpu",
          description: "Soundprint analysis, lyrics and volume levelling.",
        },
        {
          id: "ai",
          label: "AI",
          icon: Sparkles,
          component: AiSettings,
          keywords:
            "ai ollama llm language model muse prompts re-rank rerank recommendations reasons why lyrics themes embeddings qwen gpu unload skips learning",
          description:
            "Optional local AI (Ollama) for smarter Muse and recommendations.",
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
          id: "vod",
          label: "VOD",
          icon: Film,
          component: VodSettings,
          keywords:
            "vod movies on demand pluto films m3u live tv tmdb free streaming save download library queue",
          description:
            "Movies and shows from Dispatcharr (Xtream Codes) or an M3U playlist such as Pluto TV's, saving them to the library, and the download queue.",
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
          id: "notifications",
          label: "Notifications",
          icon: Bell,
          component: NotificationsSettings,
          keywords:
            "email smtp mail alerts notifications disk space task failed backup recording sign-in password reset forgot pin link",
          description:
            "Email for password resets, and alerts when something goes wrong.",
        },
        {
          id: "plex-import",
          label: "Plex",
          icon: Download,
          component: PlexImportSettings,
          keywords: "plex import sync two-way watch history ratings resume scrobble migrate accounts path mapping",
          description:
            "Keep watch status and ratings in step with Plex, and import from it.",
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
          keywords: "now playing streams bandwidth graph cpu memory gpu server stats watch statistics top users stop",
          description: "Now playing, bandwidth, the server's CPU, memory and GPU, and watch stats.",
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

// Old section ids still open their section (bookmarks, links in alerts).
const renamedSections: Record<string, string> = { "movies-on-demand": "vod" };

export function findSection(id: string) {
  id = renamedSections[id] ?? id;
  for (const g of settingsGroups) {
    const s = g.sections.find((x) => x.id === id);
    if (s) return s;
  }
  return undefined;
}
