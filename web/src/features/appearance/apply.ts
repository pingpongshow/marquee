import createClient from "openapi-fetch";
import { unwrap } from "@/api/client";
import type { components, paths } from "@/api/schema.gen";
import { parseHex } from "./contrast";
import { themeById, themePattern, themeVars } from "./themes";

export type Appearance = components["schemas"]["Appearance"];
export type AppearanceBackground = components["schemas"]["AppearanceBackground"];

/** What a tab shows: a theme and how the background is painted. */
export type Look = { themeId: string; background: AppearanceBackground };

// Sent without the sign-in token: it isn't needed, and an expired one would be refused,
// leaving the sign-in page on the default theme.
const publicApi = createClient<paths>({ baseUrl: "/api/v1" });

export const appearanceQuery = {
  queryKey: ["appearance"],
  queryFn: () => unwrap(publicApi.GET("/appearance")),
  // Open tabs pick up a change, or a holiday starting overnight, without a reload.
  refetchOnWindowFocus: true,
  refetchInterval: 5 * 60_000,
};

/** The uploaded image's dim: the theme's own background laid over it, at this strength in
 * percent. So it heads the right way for the text on whichever theme is showing, light or
 * dark, including a holiday's. */
export function dimLayer(bg: string, strength: number): string {
  const alpha = Math.min(Math.abs(strength), 100) / 100;
  const shade = `rgb(${parseHex(bg).join(" ")} / ${alpha})`;
  return `linear-gradient(${shade}, ${shade})`;
}

/** Theme variables plus what's painted behind the page. An uploaded image replaces the
 * theme's own pattern. */
export function lookVars({ themeId, background: b }: Look): Record<string, string> {
  const theme = themeById(themeId);
  const pattern = themePattern(theme);
  const image = b.imageUrl ? `url("${b.imageUrl}")` : null;
  const fill = image && b.mode === "fill";
  return {
    ...themeVars(theme),
    "--page-layers": image ? `${dimLayer(theme.colors.bg, b.dim)}, ${image}` : pattern,
    "--page-repeat": fill ? "no-repeat" : "repeat",
    "--page-size": fill ? "cover" : "auto",
    "--page-position": fill ? "center" : "0 0",
    // Bars and panels that let the plain page show through stay solid over a picture.
    "--bar-opacity": image || pattern !== "none" ? "100%" : "95%",
    "--panel-opacity": image || pattern !== "none" ? "100%" : "50%",
  };
}

const STORAGE_KEY = "marquee.appearance"; // public/theme-boot.js reads this

export function applyLook(look: Look, remember: boolean) {
  const vars = lookVars(look);
  const root = document.documentElement;
  for (const [name, value] of Object.entries(vars)) root.style.setProperty(name, value);
  const themeColor = themeById(look.themeId).colors.bg;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", themeColor);
  if (!remember) return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ vars, themeColor }));
  } catch {
    /* storage unavailable: the theme comes from the server on each load instead */
  }
}
