import { describe, expect, it } from "vitest";
import goIds from "../../../../server/internal/appearance/appearance.go?raw";
import { blend, contrast, parseHex } from "./contrast";
import { patternImage, patternTiles, type PatternId } from "./patterns";
import { defaultThemeId, themeById, themes, themeVars, type Theme } from "./themes";
import { dimLayer, lookVars } from "./apply";

// Vitest empties CSS imports, so index.css is read from disk.
const { readFileSync } = await import("node:fs" as string);
const css: string = readFileSync(new URL("../../index.css", import.meta.url), "utf8");

const AA = 4.5;
const AA_LARGE = 3; // also WCAG's minimum for controls and focus rings

function ratio(a: string, b: string) {
  return contrast(parseHex(a), parseHex(b));
}

function surfaces(t: Theme) {
  const c = t.colors;
  return { bg: c.bg, surface: c.surface, "surface-2": c.surface2, "surface-3": c.surface3 };
}

describe.each(themes.map((t) => [t.id, t] as const))("theme %s", (_, t) => {
  const c = t.colors;

  it("has readable body and muted text on its page and surfaces", () => {
    for (const [name, back] of Object.entries(surfaces(t))) {
      expect(ratio(c.text, back), `text on ${name}`).toBeGreaterThanOrEqual(AA);
      expect(ratio(c.muted, back), `muted on ${name}`).toBeGreaterThanOrEqual(AA);
    }
  });

  it("has readable accent buttons, at rest and hovered", () => {
    expect(ratio(c.onAccent, c.accent), "button").toBeGreaterThanOrEqual(AA);
    expect(ratio(c.onAccent, c.accentStrong), "hovered button").toBeGreaterThanOrEqual(AA);
  });

  it("has accent, danger and success text that reads on the page and cards", () => {
    for (const back of [c.bg, c.surface]) {
      expect(ratio(c.accent, back), `accent on ${back}`).toBeGreaterThanOrEqual(AA);
      expect(ratio(c.danger, back), `danger on ${back}`).toBeGreaterThanOrEqual(AA);
      expect(ratio(c.success, back), `success on ${back}`).toBeGreaterThanOrEqual(AA);
    }
  });

  it("has visible faint text, focus rings and danger buttons", () => {
    expect(ratio(c.faint, c.bg), "faint on page").toBeGreaterThanOrEqual(AA_LARGE);
    expect(ratio(c.faint, c.surface), "faint on surface").toBeGreaterThanOrEqual(AA_LARGE);
    expect(ratio(c.accent, c.bg), "focus ring on page").toBeGreaterThanOrEqual(AA_LARGE);
    expect(ratio(c.onDanger, c.danger), "danger button").toBeGreaterThanOrEqual(AA_LARGE);
  });

  it("keeps an accent that works on black, for players", () => {
    expect(ratio(c.accentOnBlack, "#000"), "accent on black").toBeGreaterThanOrEqual(AA);
    expect(ratio(c.onAccentOnBlack, c.accentOnBlack), "button on black").toBeGreaterThanOrEqual(AA);
    expect(ratio(c.onAccentOnBlack, c.accentStrongOnBlack), "hovered button on black").toBeGreaterThanOrEqual(AA);
  });

  it("keeps Now Playing dark, under its white text", () => {
    expect(ratio("#fff", c.immersive)).toBeGreaterThanOrEqual(7);
    expect(ratio("#fff", c.immersiveStart)).toBeGreaterThanOrEqual(AA);
  });

  it("leaves text readable where its pattern is painted", () => {
    if (!t.pattern) return;
    expect(t.pattern.opacity, "subtle").toBeLessThanOrEqual(0.12);
    const inked = blend(parseHex(c.bg), parseHex(t.pattern.color), t.pattern.opacity);
    expect(contrast(parseHex(c.text), inked), "text over the pattern").toBeGreaterThanOrEqual(AA);
    expect(contrast(parseHex(c.muted), inked), "muted over the pattern").toBeGreaterThanOrEqual(AA);
  });
});

describe("High Contrast", () => {
  it("clears AAA for text", () => {
    const t = themeById("high-contrast");
    for (const back of Object.values(surfaces(t))) {
      expect(ratio(t.colors.text, back)).toBeGreaterThanOrEqual(7);
      expect(ratio(t.colors.muted, back)).toBeGreaterThanOrEqual(7);
    }
  });
});

describe("the list of themes", () => {
  it("matches the server's", () => {
    const list = goIds.slice(goIds.indexOf("var Themes"), goIds.indexOf("// KnownTheme"));
    const server = [...list.matchAll(/"([a-z-]+)"/g)].map((m) => m[1]);
    expect(themes.map((t) => t.id).sort()).toEqual([...server].sort());
  });

  it("has no repeated ids, and every group", () => {
    expect(new Set(themes.map((t) => t.id)).size).toBe(themes.length);
    for (const group of ["styles", "seasons", "holidays"]) expect(themes.some((t) => t.group === group)).toBe(true);
  });

  it("falls back to the default for an unknown id", () => {
    expect(themeById("from-a-newer-server").id).toBe(defaultThemeId);
  });

  it("has a Daylight that really is light", () => {
    const t = themeById("daylight");
    expect(t.scheme).toBe("light");
    expect(themeVars(t)["--theme-scheme"]).toBe("light");
    expect(ratio(t.colors.bg, "#000")).toBeGreaterThan(15);
  });
});

// The default theme must be exactly what index.css has always said.
describe("Marquee", () => {
  const vars = themeVars(themeById("marquee"));
  const block = css.slice(css.indexOf("@theme static"), css.indexOf("html {"));
  const onBlack = css.slice(css.indexOf(".on-black {"), css.indexOf("/* Year in Music"));

  it("matches the defaults in index.css", () => {
    const declared = [...block.matchAll(/(--[a-z0-9-]+):\s*([^;]+);/g)];
    expect(declared.length).toBeGreaterThan(20);
    for (const [, name, value] of declared) expect(vars[name!], name).toBe(value);
  });

  it("is unchanged inside players", () => {
    const declared = [...onBlack.matchAll(/(--color-[a-z0-9-]+):\s*([^;]+);/g)];
    expect(declared.length).toBeGreaterThan(10);
    for (const [, name, value] of declared) {
      // A reference to an on-black token must land on the same colour as the theme's own.
      const reference = value!.match(/^var\((--[a-z0-9-]+)\)$/);
      expect(reference ? vars[reference[1]!] : value, name).toBe(vars[name!]);
    }
    expect(onBlack).toContain("--color-accent-strong: var(--color-accent-strong-on-black)");
  });

  it("lets a text colour on a player's own element win", () => {
    expect(css).toMatch(/@layer base \{\s*\.on-black \{/);
  });

  it("is today's look: no pattern, today's corners, the browser's scrollbars", () => {
    const look = lookVars({ themeId: "marquee", background: { mode: "tile", dim: -40 } });
    expect(look["--page-layers"]).toBe("none");
    expect(look["--bar-opacity"]).toBe("95%");
    expect(look["--panel-opacity"]).toBe("50%");
    expect(look["--radius-md"]).toBe("0.375rem");
    expect(look["--radius-lg"]).toBe("0.5rem");
    expect(look["--theme-scheme"]).toBe("normal");
    expect(look["--theme-scrollbar"]).toBe("auto");
  });
});

describe("the page background", () => {
  it("replaces a theme's pattern, tiled by default", () => {
    const pattern = lookVars({ themeId: "halloween", background: { mode: "tile", dim: 0 } });
    expect(pattern["--page-layers"]).toContain("data:image/svg+xml");
    const look = lookVars({ themeId: "halloween", background: { mode: "tile", dim: -40, imageUrl: "/x.png" } });
    expect(look["--page-layers"]).not.toContain("svg");
    expect(look["--page-layers"]).toContain('url("/x.png")');
    expect(look["--page-repeat"]).toBe("repeat");
    expect(look["--page-size"]).toBe("auto");
    expect(look["--bar-opacity"]).toBe("100%");
  });

  it("can fill the page", () => {
    const look = lookVars({ themeId: "marquee", background: { mode: "fill", dim: 0, imageUrl: "/x.png" } });
    expect(look["--page-size"]).toBe("cover");
    expect(look["--page-repeat"]).toBe("no-repeat");
  });

  it("fades towards the theme's own background, whichever way that is", () => {
    const layers = (themeId: string) => lookVars({ themeId, background: { mode: "tile", dim: 40, imageUrl: "/x.png" } })["--page-layers"];
    expect(layers("christmas")).toContain("rgb(12 31 23 / 0.4)");
    expect(layers("daylight")).toContain("rgb(246 245 241 / 0.4)");
    for (const t of themes) {
      const [r, g, b] = parseHex(t.colors.bg);
      expect(dimLayer(t.colors.bg, 60), t.id).toContain(`rgb(${r} ${g} ${b} / 0.6)`);
    }
  });

  it("reads a value stored with a sign as its size", () => {
    expect(dimLayer("#000000", -40)).toBe(dimLayer("#000000", 40));
  });
});

describe("patterns", () => {
  it("are inline images that fetch nothing", () => {
    for (const id of Object.keys(patternTiles) as PatternId[]) {
      const image = patternImage(id, "#fff", 0.1);
      expect(image.startsWith('url("data:image/svg+xml,')).toBe(true);
      const svg = decodeURIComponent(image.slice(5, -2).replace("data:image/svg+xml,", ""));
      expect(svg, id).not.toMatch(/href|<image|<script|@import/);
    }
  });

  it("have no animation", () => {
    for (const { body } of Object.values(patternTiles)) expect(body).not.toMatch(/animate|@keyframes/);
  });

  it("are all used by a theme", () => {
    const used = new Set(themes.map((t) => t.pattern?.id));
    for (const id of Object.keys(patternTiles)) expect(used.has(id as PatternId), id).toBe(true);
  });
});
