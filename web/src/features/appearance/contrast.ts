// Colour helpers (USER-20): WCAG 2 contrast for the themes test. Colours are #rgb or #rrggbb.

export type Rgb = readonly [number, number, number];

export function parseHex(hex: string): Rgb {
  const m = hex.replace("#", "");
  const full = m.length === 3 ? [...m].map((c) => c + c).join("") : m;
  const v = parseInt(full, 16);
  if (full.length !== 6 || Number.isNaN(v)) throw new Error(`not a hex colour: ${hex}`);
  return [(v >> 16) & 255, (v >> 8) & 255, v & 255];
}

function luminance([r, g, b]: Rgb) {
  const f = (c: number) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}

export function contrast(a: Rgb, b: Rgb): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

/** `over` painted on `under` at the given opacity. */
export function blend(under: Rgb, over: Rgb, opacity: number): Rgb {
  return under.map((u, i) => u + (over[i]! - u) * opacity) as unknown as Rgb;
}
