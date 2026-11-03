// The web app's themes (USER-20). The server only knows their ids
// (server/internal/appearance/appearance.go); a test keeps the two lists matched, and checks
// every theme's text and accent colours against WCAG AA.

import { patternImage, type PatternId } from "./patterns";

export type ThemeGroup = "styles" | "seasons" | "holidays";

export type ThemeColors = {
  bg: string;
  surface: string;
  surface2: string;
  surface3: string;
  border: string;
  text: string;
  muted: string;
  faint: string;
  accent: string;
  accentStrong: string;
  onAccent: string;
  accentOnBlack: string;
  accentStrongOnBlack: string;
  onAccentOnBlack: string;
  danger: string;
  onDanger: string;
  success: string;
  warning: string;
  ok: string;
  charts: readonly [string, string, string, string, string, string, string];
  immersive: string;
  immersiveStart: string;
  authGlow: string;
};

export type Theme = {
  id: string;
  name: string;
  group: ThemeGroup;
  /** Light themes also get light form controls and scrollbars. */
  scheme: "dark" | "light";
  colors: ThemeColors;
  /** Multiplies every corner's roundness. */
  roundness: number;
  font: string;
  /** Faint page decoration; an uploaded background replaces it. */
  pattern?: { id: PatternId; color: string; opacity: number };
};

/** System and web-safe stacks only: a theme never makes a font request. */
const fonts = {
  standard: '"Inter", ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif',
  serif: 'Georgia, "Iowan Old Style", "Times New Roman", serif',
  book: '"Palatino Linotype", Palatino, "Book Antiqua", Georgia, serif',
  rounded: 'ui-rounded, "SF Pro Rounded", "Arial Rounded MT Bold", "Trebuchet MS", ui-sans-serif, sans-serif',
  geometric: '"Avenir Next", Avenir, "Century Gothic", "Trebuchet MS", ui-sans-serif, sans-serif',
  humanist: '"Gill Sans", "Gill Sans MT", Optima, "Trebuchet MS", ui-sans-serif, sans-serif',
};

/** The default theme's Tailwind colours, kept for the other dark themes. */
const darkCharts = [
  "oklch(74.6% 0.16 232.661)",
  "oklch(76.5% 0.177 163.223)",
  "oklch(82.8% 0.189 84.429)",
  "oklch(70.2% 0.183 293.541)",
  "oklch(71.2% 0.194 13.428)",
  "oklch(77.7% 0.152 181.912)",
  "oklch(84.1% 0.238 128.85)",
] as const;
const lightCharts = ["#0284c7", "#059669", "#d97706", "#7c3aed", "#e11d48", "#0d9488", "#65a30d"] as const;

type Palette = Pick<ThemeColors, "bg" | "surface" | "surface2" | "surface3" | "border" | "text" | "muted" | "faint" | "accent" | "accentStrong" | "onAccent" | "danger" | "success"> & Partial<ThemeColors>;
type Style = { roundness?: number; font?: keyof typeof fonts; pattern?: Theme["pattern"] };

function dark(id: string, name: string, group: ThemeGroup, palette: Palette, style: Style = {}): Theme {
  return {
    id,
    name,
    group,
    scheme: "dark",
    roundness: style.roundness ?? 1,
    font: fonts[style.font ?? "standard"],
    pattern: style.pattern,
    colors: {
      accentOnBlack: palette.accent,
      accentStrongOnBlack: palette.accentStrong,
      onAccentOnBlack: palette.onAccent,
      onDanger: palette.bg,
      warning: "oklch(76.9% 0.188 70.08)",
      ok: "oklch(72.3% 0.219 149.579)",
      charts: darkCharts,
      immersive: "#0b0b0f",
      immersiveStart: palette.surface3,
      authGlow: palette.surface2,
      ...palette,
    },
  };
}

function light(id: string, name: string, group: ThemeGroup, palette: Palette, style: Style = {}): Theme {
  return {
    ...dark(id, name, group, palette, style),
    scheme: "light",
    colors: {
      accentOnBlack: palette.accent,
      accentStrongOnBlack: palette.accentStrong,
      onAccentOnBlack: palette.onAccent,
      onDanger: "#fff",
      warning: "#a16207",
      ok: "#15803d",
      charts: lightCharts,
      immersive: "#17181d",
      immersiveStart: "#3a3a44",
      authGlow: palette.surface3,
      ...palette,
    },
  };
}

export const defaultThemeId = "marquee";

export const themes: Theme[] = [
  // Styles
  dark("marquee", "Marquee", "styles", {
    bg: "#121316",
    surface: "#1b1d22",
    surface2: "#23262d",
    surface3: "#2c3038",
    border: "#343843",
    text: "#eceef2",
    muted: "#9aa1ad",
    faint: "#6b7280",
    accent: "#f2b134",
    accentStrong: "#ffc451",
    onAccent: "#000",
    danger: "#ef5b5b",
    onDanger: "#fff",
    success: "#46c287",
    immersiveStart: "#3a3a44",
    authGlow: "#2a2418",
  }),
  dark("midnight-blue", "Midnight Blue", "styles", {
    bg: "#0b1226",
    surface: "#121b36",
    surface2: "#1a2547",
    surface3: "#233059",
    border: "#2f3d6b",
    text: "#e8edfb",
    muted: "#a3b0d3",
    faint: "#7484ad",
    accent: "#6ea8ff",
    accentStrong: "#8fbcff",
    onAccent: "#07122b",
    danger: "#ff7b7b",
    success: "#4fd1a1",
    immersive: "#050914",
  }, { pattern: { id: "stars", color: "#c9d8ff", opacity: 0.09 } }),
  dark("true-black", "True Black (OLED)", "styles", {
    bg: "#000000",
    surface: "#0c0c0c",
    surface2: "#161616",
    surface3: "#202020",
    border: "#2e2e2e",
    text: "#f2f2f2",
    muted: "#a6a6a6",
    faint: "#757575",
    accent: "#f2b134",
    accentStrong: "#ffc451",
    onAccent: "#000",
    danger: "#ff5c5c",
    success: "#4cd08f",
    immersive: "#000000",
    authGlow: "#141414",
  }),
  light("daylight", "Daylight", "styles", {
    bg: "#f6f5f1",
    surface: "#ffffff",
    surface2: "#efeee9",
    surface3: "#e3e2dc",
    border: "#cfcdc5",
    text: "#1c1d20",
    muted: "#55585f",
    faint: "#74777d",
    accent: "#a8560a",
    accentStrong: "#8f4807",
    onAccent: "#ffffff",
    accentOnBlack: "#f2b134",
    accentStrongOnBlack: "#ffc451",
    onAccentOnBlack: "#000",
    danger: "#b91c1c",
    success: "#15803d",
  }),
  dark("vintage-cinema", "Vintage Cinema", "styles", {
    bg: "#1c140c",
    surface: "#281d12",
    surface2: "#33261a",
    surface3: "#3f3022",
    border: "#54412e",
    text: "#f3e6cf",
    muted: "#c2ad8b",
    faint: "#8f7c60",
    accent: "#d9a05b",
    accentStrong: "#e8b678",
    onAccent: "#1c140c",
    danger: "#ec8a74",
    success: "#a9bd66",
    immersive: "#120c06",
  }, { roundness: 0.5, font: "serif", pattern: { id: "film", color: "#f3e6cf", opacity: 0.045 } }),
  dark("neon", "Neon", "styles", {
    bg: "#0a0614",
    surface: "#140c24",
    surface2: "#1e1236",
    surface3: "#2a1a48",
    border: "#43286e",
    text: "#f2edff",
    muted: "#b6a8d9",
    faint: "#8574ad",
    accent: "#ff2fa8",
    accentStrong: "#ff66c0",
    onAccent: "#12001a",
    danger: "#ff7088",
    success: "#2fffc0",
    charts: ["#22d3ee", "#2fffc0", "#fde047", "#c084fc", "#ff66c0", "#5eead4", "#a3e635"],
    immersive: "#06030d",
  }, { font: "geometric", pattern: { id: "grid", color: "#c084fc", opacity: 0.08 } }),
  dark("forest", "Forest", "styles", {
    bg: "#0f1a13",
    surface: "#16241b",
    surface2: "#1e3025",
    surface3: "#283d30",
    border: "#36503f",
    text: "#e8f2ea",
    muted: "#a3bcaa",
    faint: "#728d7a",
    accent: "#7fc27a",
    accentStrong: "#98d393",
    onAccent: "#0a1a0d",
    danger: "#f28472",
    success: "#7fdca0",
    immersive: "#08110b",
  }, { font: "humanist", pattern: { id: "pines", color: "#b9dcb9", opacity: 0.06 } }),
  dark("ocean", "Ocean", "styles", {
    bg: "#08191f",
    surface: "#0e2530",
    surface2: "#15313e",
    surface3: "#1d3f4e",
    border: "#2a5263",
    text: "#e5f5f8",
    muted: "#9dc0ca",
    faint: "#6c929e",
    accent: "#2fc4d4",
    accentStrong: "#5ed6e2",
    onAccent: "#021a1f",
    danger: "#ff8a7d",
    success: "#52d9a6",
    immersive: "#041014",
  }, { pattern: { id: "waves", color: "#9fe3ee", opacity: 0.07 } }),
  dark("rose", "Rosé", "styles", {
    bg: "#1d1216",
    surface: "#2a1a20",
    surface2: "#36222a",
    surface3: "#432b35",
    border: "#5a3a47",
    text: "#fbecef",
    muted: "#d2b1bb",
    faint: "#a3808c",
    accent: "#f29bb2",
    accentStrong: "#f7b6c7",
    onAccent: "#2a0d17",
    danger: "#ff8d80",
    success: "#8ed8a6",
    immersive: "#120a0d",
  }, { roundness: 1.5, font: "humanist", pattern: { id: "petals", color: "#f7c5d2", opacity: 0.06 } }),
  dark("nord", "Nord", "styles", {
    bg: "#2e3440",
    surface: "#3b4252",
    surface2: "#434c5e",
    surface3: "#4c566a",
    border: "#5e6a82",
    text: "#eceff4",
    muted: "#d8dee9",
    faint: "#aeb7c6",
    accent: "#88c0d0",
    accentStrong: "#8fbcbb",
    onAccent: "#2e3440",
    danger: "#f0a0a6",
    success: "#b5d09e",
    warning: "#ebcb8b",
    ok: "#a3be8c",
    charts: ["#88c0d0", "#a3be8c", "#ebcb8b", "#b48ead", "#d08770", "#8fbcbb", "#d8dee9"],
    immersive: "#1f242d",
  }),
  dark("high-contrast", "High Contrast", "styles", {
    bg: "#000000",
    surface: "#0a0a0a",
    surface2: "#141414",
    surface3: "#1f1f1f",
    border: "#ffffff",
    text: "#ffffff",
    muted: "#e6e6e6",
    faint: "#c0c0c0",
    accent: "#ffd500",
    accentStrong: "#ffe44d",
    onAccent: "#000",
    danger: "#ff8080",
    success: "#4dff9d",
    warning: "#ffb020",
    ok: "#4dff9d",
    immersive: "#000000",
    immersiveStart: "#1f1f1f",
    authGlow: "#0a0a0a",
  }),

  // Seasons
  light("spring", "Spring", "seasons", {
    bg: "#f4f8ef",
    surface: "#ffffff",
    surface2: "#ecf3e4",
    surface3: "#dfe9d4",
    border: "#c5d3b7",
    text: "#1e2a1a",
    muted: "#4e5e47",
    faint: "#6c7a65",
    accent: "#2f7d32",
    accentStrong: "#27692a",
    onAccent: "#ffffff",
    accentOnBlack: "#7fc27a",
    accentStrongOnBlack: "#98d393",
    onAccentOnBlack: "#0a1a0d",
    danger: "#b3261e",
    success: "#2e7d32",
    immersive: "#14201a",
  }, { roundness: 1.5, font: "rounded", pattern: { id: "blossoms", color: "#d65f91", opacity: 0.09 } }),
  light("summer", "Summer", "seasons", {
    bg: "#fff9ec",
    surface: "#ffffff",
    surface2: "#fdf0d3",
    surface3: "#f8e4b8",
    border: "#e6cf9a",
    text: "#2b2110",
    muted: "#5f4f2e",
    faint: "#7a6946",
    accent: "#c2410c",
    accentStrong: "#a8380a",
    onAccent: "#ffffff",
    accentOnBlack: "#fb923c",
    accentStrongOnBlack: "#fdba74",
    onAccentOnBlack: "#1c0a00",
    danger: "#b91c1c",
    success: "#15803d",
    immersive: "#1f1608",
  }, { font: "rounded", pattern: { id: "suns", color: "#e8912d", opacity: 0.11 } }),
  dark("fall", "Fall", "seasons", {
    bg: "#1a120b",
    surface: "#261a10",
    surface2: "#322316",
    surface3: "#3f2d1c",
    border: "#553d26",
    text: "#f5e9dc",
    muted: "#c9b196",
    faint: "#96805f",
    accent: "#e8833a",
    accentStrong: "#f09a5a",
    onAccent: "#1a0d02",
    danger: "#f28d78",
    success: "#b5c766",
    immersive: "#100a05",
  }, { font: "humanist", pattern: { id: "leaves", color: "#e8a062", opacity: 0.07 } }),
  dark("winter", "Winter", "seasons", {
    bg: "#0e1620",
    surface: "#162231",
    surface2: "#1f2e40",
    surface3: "#293a4f",
    border: "#3a4f68",
    text: "#eaf3fb",
    muted: "#a9bdd0",
    faint: "#7890a8",
    accent: "#8fd3f4",
    accentStrong: "#b0e2fa",
    onAccent: "#06202e",
    danger: "#ff8f8f",
    success: "#6fdcb0",
    immersive: "#080d14",
  }, { pattern: { id: "crystals", color: "#d6ecfa", opacity: 0.07 } }),

  // Holidays
  dark("new-year", "New Year", "holidays", {
    bg: "#0d0f1f",
    surface: "#171a30",
    surface2: "#20243f",
    surface3: "#2a2f50",
    border: "#3c4268",
    text: "#f5f2e6",
    muted: "#b9b6c9",
    faint: "#868399",
    accent: "#e9c46a",
    accentStrong: "#f2d58a",
    onAccent: "#1a1400",
    danger: "#ff8080",
    success: "#6fdcb0",
    immersive: "#060812",
  }, { font: "geometric", pattern: { id: "fireworks", color: "#f2d58a", opacity: 0.08 } }),
  dark("chinese-new-year", "Chinese New Year", "holidays", {
    bg: "#2a0808",
    surface: "#3a0e0e",
    surface2: "#4a1414",
    surface3: "#5a1b1b",
    border: "#7a2a2a",
    text: "#fff0da",
    muted: "#efcfb8",
    faint: "#cb9f85",
    accent: "#f5c242",
    accentStrong: "#ffd56a",
    onAccent: "#2a0808",
    danger: "#ffb5a3",
    success: "#a9e6aa",
    warning: "#ffc46b",
    ok: "#a9e6aa",
    immersive: "#1a0404",
  }, { font: "book", pattern: { id: "lanterns", color: "#f5c242", opacity: 0.08 } }),
  dark("valentines", "Valentine's Day", "holidays", {
    bg: "#2a0f1b",
    surface: "#3a1626",
    surface2: "#4a1d31",
    surface3: "#5a253c",
    border: "#7a3552",
    text: "#ffeef3",
    muted: "#ecc6d3",
    faint: "#c598a9",
    accent: "#ff6f97",
    accentStrong: "#ff8fae",
    onAccent: "#2a0512",
    danger: "#ffb3a0",
    success: "#a0e3b4",
    warning: "#ffc46b",
    immersive: "#190810",
  }, { roundness: 1.5, font: "book", pattern: { id: "hearts", color: "#ff9db8", opacity: 0.08 } }),
  dark("mardi-gras", "Mardi Gras", "holidays", {
    bg: "#1b0f2b",
    surface: "#281740",
    surface2: "#342053",
    surface3: "#412a66",
    border: "#5a3d85",
    text: "#f6f0ff",
    muted: "#c6b7de",
    faint: "#9483ad",
    accent: "#f5c518",
    accentStrong: "#ffd84d",
    onAccent: "#1b0f2b",
    danger: "#ff8fa0",
    success: "#5fd38a",
    immersive: "#10081a",
  }, { font: "geometric", pattern: { id: "carnival", color: "#7ddc9c", opacity: 0.07 } }),
  dark("st-patricks", "St Patrick's Day", "holidays", {
    bg: "#07231a",
    surface: "#0d3226",
    surface2: "#134031",
    surface3: "#1a4f3d",
    border: "#2a6a53",
    text: "#eefbf3",
    muted: "#b5dcc8",
    faint: "#84ae99",
    accent: "#5fdc8b",
    accentStrong: "#86e8a8",
    onAccent: "#03140d",
    danger: "#ffa394",
    success: "#f2d06b",
    immersive: "#04140f",
  }, { font: "humanist", pattern: { id: "shamrocks", color: "#8ee8ae", opacity: 0.07 } }),
  light("easter", "Easter", "holidays", {
    bg: "#f8f5fb",
    surface: "#ffffff",
    surface2: "#f0ebf6",
    surface3: "#e4dcef",
    border: "#cfc4de",
    text: "#2a2233",
    muted: "#5b5068",
    faint: "#776c84",
    accent: "#7b3fa8",
    accentStrong: "#66308f",
    onAccent: "#ffffff",
    accentOnBlack: "#c9a0ea",
    accentStrongOnBlack: "#dcc0f2",
    onAccentOnBlack: "#1d0d2c",
    danger: "#b3261e",
    success: "#2e7d32",
    immersive: "#1c1426",
  }, { roundness: 1.5, font: "rounded", pattern: { id: "eggs", color: "#b28ad6", opacity: 0.11 } }),
  dark("independence-day", "Independence Day", "holidays", {
    bg: "#0b1730",
    surface: "#132243",
    surface2: "#1b2e55",
    surface3: "#243a68",
    border: "#37528a",
    text: "#f5f7fb",
    muted: "#b9c5dc",
    faint: "#8c9cba",
    accent: "#ff6b6b",
    accentStrong: "#ff8a8a",
    onAccent: "#0b1730",
    danger: "#ffa8a8",
    success: "#6fdcb0",
    immersive: "#060d1c",
  }, { pattern: { id: "starsAndStripes", color: "#e8edf8", opacity: 0.06 } }),
  dark("halloween", "Halloween", "holidays", {
    bg: "#140d1c",
    surface: "#20142c",
    surface2: "#2b1c3a",
    surface3: "#38254a",
    border: "#4f3566",
    text: "#f7efe2",
    muted: "#c5b4cf",
    faint: "#9483a0",
    accent: "#ff8c1a",
    accentStrong: "#ffa64d",
    onAccent: "#1a0b00",
    danger: "#ff8080",
    success: "#a3d65c",
    immersive: "#0a0610",
  }, { pattern: { id: "batsAndPumpkins", color: "#ffa64d", opacity: 0.1 } }),
  dark("dia-de-los-muertos", "Día de los Muertos", "holidays", {
    bg: "#1a0a24",
    surface: "#2a1136",
    surface2: "#381847",
    surface3: "#472059",
    border: "#663080",
    text: "#fff3e0",
    muted: "#dcbfe0",
    faint: "#ab8ab6",
    accent: "#ff9f1c",
    accentStrong: "#ffb74d",
    onAccent: "#2a1136",
    danger: "#ff90b3",
    success: "#7fdcae",
    immersive: "#0f0515",
  }, { font: "book", pattern: { id: "marigolds", color: "#ffb74d", opacity: 0.08 } }),
  dark("diwali", "Diwali", "holidays", {
    bg: "#24091f",
    surface: "#340f2c",
    surface2: "#43163a",
    surface3: "#531e48",
    border: "#742f65",
    text: "#fff4dc",
    muted: "#ebcdd2",
    faint: "#bd99a4",
    accent: "#ffb01f",
    accentStrong: "#ffc552",
    onAccent: "#2a0c00",
    danger: "#ff9d9d",
    success: "#8fe0a8",
    immersive: "#150512",
  }, { font: "book", pattern: { id: "diyas", color: "#ffc552", opacity: 0.08 } }),
  dark("thanksgiving", "Thanksgiving", "holidays", {
    bg: "#1e130a",
    surface: "#2c1c0f",
    surface2: "#3a2715",
    surface3: "#48321c",
    border: "#62462a",
    text: "#f8ecd9",
    muted: "#cfb795",
    faint: "#9b8564",
    accent: "#e2a03f",
    accentStrong: "#ebb564",
    onAccent: "#1e130a",
    danger: "#f0907a",
    success: "#b5c766",
    immersive: "#120b05",
  }, { font: "serif", pattern: { id: "harvest", color: "#e2b97a", opacity: 0.07 } }),
  dark("hanukkah", "Hanukkah", "holidays", {
    bg: "#0a1a3a",
    surface: "#102552",
    surface2: "#173166",
    surface3: "#1f3d7a",
    border: "#2f5496",
    text: "#f2f6ff",
    muted: "#c0d0ec",
    faint: "#8fa5cd",
    accent: "#a8d0ff",
    accentStrong: "#c5e0ff",
    onAccent: "#081a3a",
    danger: "#ffa8a8",
    success: "#7fe0bc",
    immersive: "#060f24",
  }, { font: "book", pattern: { id: "menorahs", color: "#cfe3ff", opacity: 0.07 } }),
  dark("christmas", "Christmas", "holidays", {
    bg: "#0c1f17",
    surface: "#122b20",
    surface2: "#193729",
    surface3: "#204433",
    border: "#2f5c46",
    text: "#f7f3e8",
    muted: "#bdd0c2",
    faint: "#8aa394",
    accent: "#f4655f",
    accentStrong: "#f7837e",
    onAccent: "#140000",
    danger: "#ffb199",
    success: "#f2d06b",
    immersive: "#06120d",
    authGlow: "#3a1412",
  }, { font: "serif", pattern: { id: "snowAndTrees", color: "#e8f3ec", opacity: 0.07 } }),
];

const byId = new Map(themes.map((t) => [t.id, t]));

/** A theme by id; an unknown id (a newer server, say) is the default. */
export function themeById(id: string): Theme {
  return byId.get(id) ?? byId.get(defaultThemeId)!;
}

/** The CSS variables that make up a theme, on <html> or on a preview in the gallery. */
export function themeVars(t: Theme): Record<string, string> {
  const c = t.colors;
  const rem = (v: number) => `${+(v * t.roundness).toFixed(4)}rem`;
  // The default theme keeps the browser's own scrollbars and form controls.
  const isDefault = t.id === defaultThemeId;
  return {
    "--color-bg": c.bg,
    "--color-surface": c.surface,
    "--color-surface-2": c.surface2,
    "--color-surface-3": c.surface3,
    "--color-border": c.border,
    "--color-text": c.text,
    "--color-muted": c.muted,
    "--color-faint": c.faint,
    "--color-accent": c.accent,
    "--color-accent-strong": c.accentStrong,
    "--color-on-accent": c.onAccent,
    "--color-accent-on-black": c.accentOnBlack,
    "--color-accent-strong-on-black": c.accentStrongOnBlack,
    "--color-on-accent-on-black": c.onAccentOnBlack,
    "--color-danger": c.danger,
    "--color-on-danger": c.onDanger,
    "--color-success": c.success,
    "--color-warning": c.warning,
    "--color-ok": c.ok,
    ...Object.fromEntries(c.charts.map((v, i) => [`--color-chart-${i + 1}`, v])),
    "--color-immersive": c.immersive,
    "--color-immersive-start": c.immersiveStart,
    "--color-auth-glow": c.authGlow,
    "--font-sans": t.font,
    "--radius": rem(0.25),
    "--radius-xs": rem(0.125),
    "--radius-sm": rem(0.25),
    "--radius-md": rem(0.375),
    "--radius-lg": rem(0.5),
    "--radius-xl": rem(0.75),
    "--radius-2xl": rem(1),
    "--radius-3xl": rem(1.5),
    "--theme-scheme": isDefault ? "normal" : t.scheme,
    "--on-black-scheme": isDefault ? "normal" : "dark",
    "--theme-scrollbar": isDefault ? "auto" : `${c.surface3} ${c.bg}`,
  };
}

/** The theme's pattern as a CSS image, or none. */
export function themePattern(t: Theme): string {
  return t.pattern ? patternImage(t.pattern.id, t.pattern.color, t.pattern.opacity) : "none";
}

export const themeGroups: { id: ThemeGroup; label: string; help?: string }[] = [
  { id: "styles", label: "Styles" },
  { id: "seasons", label: "Seasons", help: "Seasons only show when you choose them." },
  { id: "holidays", label: "Holidays" },
];
