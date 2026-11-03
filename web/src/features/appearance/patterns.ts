// Decorative page patterns (USER-20), drawn here as inline SVG so nothing is fetched. Each is
// a tile in one colour, painted faintly behind the page; currentColor is the theme's choice.

export type PatternTile = { size: number; body: string };

const n = (v: number) => +v.toFixed(2);

/** A star with `points` points, centred on x, y. */
function star(x: number, y: number, r: number, points = 5, inner = 0.42) {
  const pts: string[] = [];
  for (let i = 0; i < points * 2; i++) {
    const a = (Math.PI * i) / points - Math.PI / 2;
    const d = i % 2 ? r * inner : r;
    pts.push(`${n(x + d * Math.cos(a))},${n(y + d * Math.sin(a))}`);
  }
  return `<polygon points="${pts.join(" ")}"/>`;
}

const at = (x: number, y: number, body: string, rotate = 0, scale = 1) =>
  `<g transform="translate(${x} ${y})${rotate ? ` rotate(${rotate})` : ""}${scale !== 1 ? ` scale(${scale})` : ""}">${body}</g>`;

const dot = (x: number, y: number, r = 1.5) => `<circle cx="${x}" cy="${y}" r="${r}"/>`;

const line = "fill='none' stroke='currentColor' stroke-width='1.6' stroke-linecap='round' stroke-linejoin='round'";

/** Points around a circle, for petals, rays and spokes. */
const around = (count: number, draw: (angle: number) => string) =>
  Array.from({ length: count }, (_, i) => draw((360 * i) / count)).join("");

const flake = (r: number) =>
  `<g ${line}>${around(6, (a) => `<path transform="rotate(${a})" d="M0 0V${-r}M0 ${n(-r * 0.6)}l${n(r * 0.28)} ${n(-r * 0.24)}M0 ${n(-r * 0.6)}l${n(-r * 0.28)} ${n(-r * 0.24)}"/>`)}</g>`;

const blossom = (r: number) =>
  around(5, (a) => `<ellipse transform="rotate(${a})" cx="0" cy="${n(-r * 0.62)}" rx="${n(r * 0.42)}" ry="${n(r * 0.6)}"/>`);

const heart = "M0 12C-14 2-12-8-5-8c3 0 4 2 5 4 1-2 2-4 5-4 7 0 9 10-5 20Z";
const leaf = `<path d="M0-16C10-9 11 7 0 16-11 7-10-9 0-16Z"/><path d="M0 16V24" ${line}/>`;
const shamrock = `<path d="M0 0C-10-3-11-14-4-15c2 0 3 1 4 3 1-2 2-3 4-3 7 1 6 12-4 15Z"/><path transform="rotate(115)" d="M0 0C-10-3-11-14-4-15c2 0 3 1 4 3 1-2 2-3 4-3 7 1 6 12-4 15Z"/><path transform="rotate(-115)" d="M0 0C-10-3-11-14-4-15c2 0 3 1 4 3 1-2 2-3 4-3 7 1 6 12-4 15Z"/><path d="M0 0C1 6 3 11 6 15" ${line}/>`;
const egg = "M0-16C8-16 12-4 12 4 12 12 7 16 0 16-7 16-12 12-12 4-12-4-8-16 0-16Z";
const bat = "M0-4 2-8 4-4C8-7 16-8 22-2 18-2 16 1 16 5 13 2 10 2 8 6 6 3 3 3 0 7-3 3-6 3-8 6-10 2-13 2-16 5-16 1-18-2-22-2-16-8-8-7-4-4L-2-8Z";
const pumpkin = `<g ${line}><ellipse rx="16" ry="13"/><ellipse rx="9" ry="13"/><ellipse rx="3" ry="13"/><path d="M0-13C0-17 2-19 5-20"/></g><path d="M-9-4-5-4-7-8ZM5-4 9-4 7-8ZM-8 4-4 6 0 4 4 6 8 4 4 9 0 7-4 9Z"/>`;
const lantern = `<rect x="-6" y="-19" width="12" height="3" rx="1"/><path d="M-14-3C-14-11-8-16 0-16 8-16 14-11 14-3 14 5 8 10 0 10-8 10-14 5-14-3Z"/><rect x="-6" y="10" width="12" height="3" rx="1"/><path d="M0-25V-19M-3 13V23M0 13V25M3 13V23" ${line}/>`;
const cloud = `<path d="M-20 4C-20-2-12-3-11-1-9-7 3-7 3-1 7-4 14-2 13 3 18 3 18 8 13 8H-16C-21 8-22 5-20 4Z" ${line}/>`;
const burst = (r: number) =>
  `<g ${line}>${around(12, (a) => `<path transform="rotate(${a})" d="M0 ${n(-r * 0.35)}V${-r}"/>`)}</g>${around(12, (a) => `<circle transform="rotate(${a + 15})" cx="0" cy="${n(-r * 0.78)}" r="1.1"/>`)}`;
const mask = `<path fill-rule="evenodd" d="M-22-7C-14-13-5-11 0-6 5-11 14-13 22-7 22 5 12 11 5 6 3 4-3 4-5 6-12 11-22 5-22-7ZM-14-2C-11-5-7-4-5-1-8 1-12 1-14-2ZM14-2C11-5 7-4 5-1 8 1 12 1 14-2Z"/>`;
const fleur = `<path d="M0-18C5-11 6-5 2 2H-2C-6-5-5-11 0-18ZM3 3C5-4 13-8 16-2 18 3 12 7 9 4 11 2 10 0 8 1 6 2 6 4 4 6ZM-3 3C-5-4-13-8-16-2-18 3-12 7-9 4-11 2-10 0-8 1-6 2-6 4-4 6Z"/><rect x="-7" y="6" width="14" height="3" rx="1"/><path d="M-2 9H2L4 17C2 15 1 14 0 13-1 14-2 15-4 17Z"/>`;
const skull = `<path fill-rule="evenodd" d="M-12-3C-12-16 12-16 12-3 12 3 9 6 7 7V13H-7V7C-9 6-12 3-12-3ZM-8-2A3.5 3.5 0 1 0-1-2 3.5 3.5 0 1 0-8-2ZM1-2A3.5 3.5 0 1 0 8-2 3.5 3.5 0 1 0 1-2ZM0 2-2 6H2Z"/>`;
const marigold = `${around(12, (a) => `<circle transform="rotate(${a})" cx="0" cy="-10" r="3.6"/>`)}${around(8, (a) => `<circle transform="rotate(${a + 20})" cx="0" cy="-5" r="2.6"/>`)}`;
const diya = `<path d="M-16 2C-12 13 12 13 16 2 8 5-8 5-16 2Z"/><path d="M0-16C5-9 5-3 0 0-5-3-5-9 0-16Z"/>`;
const rangoli = `${around(8, (a) => `<path transform="rotate(${a})" d="M0-6C4-11 4-17 0-22-4-17-4-11 0-6Z"/>`)}${dot(0, 0, 3)}${around(8, (a) => `<circle transform="rotate(${a + 22.5})" cx="0" cy="-20" r="1.5"/>`)}`;
const wheat = `<path d="M0 26V-20" ${line}/>${[-16, -8, 0, 8].map((y) => `<ellipse transform="translate(5 ${y}) rotate(35)" rx="2.6" ry="5.5"/><ellipse transform="translate(-5 ${y}) rotate(-35)" rx="2.6" ry="5.5"/>`).join("")}<ellipse cy="-22" rx="2.6" ry="5.5"/>`;
const acorn = `<path d="M-10-3H10C10 8 4 13 0 16-4 13-10 8-10-3Z"/><path d="M-12-5C-12-12 12-12 12-5ZM0-11V-15" ${line}/><path d="M-12-5C-12-12 12-12 12-5Z"/>`;
const davidStar = `<g ${line}><path d="M0-17 15 9H-15Z"/><path d="M0 17-15-9H15Z"/></g>`;
const menorah = `<g ${line}><path d="M0-12V16M-8 18H8"/>${[8, 16, 24].map((r) => `<path d="M${-r}-12V-8A${r} ${r} 0 0 0 ${r}-8V-12"/>`).join("")}</g>${[-24, -16, -8, 8, 16, 24].map((x) => `<path transform="translate(${x} -15)" d="M0-5C2-3 2-1 0 1-2-1-2-3 0-5Z"/>`).join("")}<path transform="translate(0 -17)" d="M0-5C2-3 2-1 0 1-2-1-2-3 0-5Z"/>`;
const tree = `<g ${line}><path d="M0-22 10-8H5L14 6H-14L-5-8H-10ZM0 6V14"/></g>${star(0, -25, 4)}`;
const pine = `<path d="M0-24 11-8H6L15 8H4V14H-4V8H-15L-6-8H-11Z"/>`;
const wave = `<path d="M0 0C10-12 20-12 30 0S50 12 60 0" ${line}/>`;
const sun = `<circle r="9"/><g ${line}>${around(10, (a) => `<path transform="rotate(${a})" d="M0-13V-20"/>`)}</g>`;

export const patternTiles = {
  stars: {
    size: 140,
    body: star(30, 34, 5) + star(104, 88, 4) + dot(82, 22, 1.2) + dot(18, 104, 1.4) + dot(122, 30, 1) + dot(60, 118, 1),
  },
  film: {
    size: 72,
    body: [6, 42].map((y) => `<rect x="4" y="${y}" width="12" height="18" rx="2.5"/><rect x="56" y="${y}" width="12" height="18" rx="2.5"/>`).join("") + `<path d="M22 0V72M50 0V72" ${line}/>`,
  },
  grid: { size: 56, body: `<path d="M0 .5H56M.5 0V56" ${line}/>` + dot(.5, .5, 2) },
  pines: { size: 150, body: at(38, 46, pine) + at(112, 108, pine, 0, 1.35) + at(112, 34, pine, 0, 0.6) + dot(30, 118) + dot(70, 84) },
  waves: { size: 120, body: `<g transform="translate(0 24)">${wave}${at(60, 0, wave)}</g><g transform="translate(-30 64)">${wave}${at(60, 0, wave)}${at(120, 0, wave)}</g><g transform="translate(0 104)">${wave}${at(60, 0, wave)}</g>` },
  petals: {
    size: 130,
    body: at(34, 36, `<ellipse rx="6" ry="11"/>`, 35) + at(96, 58, `<ellipse rx="5" ry="10"/>`, -40) + at(52, 100, `<ellipse rx="5.5" ry="10"/>`, 70) + at(112, 112, `<ellipse rx="4" ry="8"/>`, 15),
  },
  blossoms: { size: 140, body: at(38, 40, blossom(15)) + at(106, 100, blossom(11), 30) + dot(104, 34, 2) + dot(36, 110, 2) + dot(70, 72, 1.5) },
  suns: { size: 150, body: at(42, 44, sun) + at(112, 112, sun, 18, 0.7) + at(30, 116, wave, 0, 0.5) + at(100, 34, wave, 0, 0.4) },
  leaves: { size: 150, body: at(40, 40, leaf, 35) + at(112, 56, leaf, -50, 0.8) + at(70, 114, leaf, 110, 0.95) + dot(112, 120, 2) + dot(14, 110, 2) },
  crystals: { size: 150, body: at(42, 44, flake(19)) + at(112, 108, flake(14)) + dot(110, 32, 1.5) + dot(34, 116, 2) + dot(74, 80, 1.3) },
  fireworks: { size: 170, body: at(52, 54, burst(28)) + at(130, 128, burst(20)) + star(130, 44, 5) + star(42, 136, 4) + dot(90, 100, 1.5) },
  lanterns: { size: 170, body: at(44, 52, lantern) + at(126, 122, lantern, 0, 0.85) + at(124, 40, cloud) + at(42, 134, cloud, 0, 0.85) },
  hearts: { size: 130, body: at(36, 34, `<path d="${heart}"/>`, -12) + at(100, 92, `<path d="${heart}"/>`, 14, 0.8) + at(102, 28, `<path d="${heart}"/>`, 8, 0.4) + at(30, 104, `<path d="${heart}"/>`, -10, 0.5) },
  carnival: { size: 170, body: at(50, 44, mask, -8) + at(126, 118, fleur) + at(120, 38, `<rect x="-5" y="-5" width="10" height="10" transform="rotate(45)"/>`) + at(40, 128, `<rect x="-7" y="-7" width="14" height="14" transform="rotate(45)"/>`) + [0, 1, 2, 3].map((i) => dot(76 + i * 8, 84 - i * 6, 2)).join("") },
  shamrocks: { size: 140, body: at(40, 44, shamrock) + at(108, 104, shamrock, 20, 0.8) + at(108, 32, shamrock, -15, 0.45) + dot(36, 108, 2) },
  eggs: {
    size: 140,
    body: at(40, 46, `<g ${line}><path d="${egg}"/><path d="M-11-2-6 2-2-2 2 2 6-2 11 2"/><path d="M-12 7H12"/></g>`, -14) + at(104, 100, `<path d="${egg}"/>`, 16, 0.9) + at(102, 36, `<g ${line}><path d="${egg}"/></g>${dot(0, -3, 2)}${dot(-5, 4, 2)}${dot(5, 4, 2)}`, 10, 0.75) + at(30, 112, `<path d="${egg}"/>`, -8, 0.5),
  },
  starsAndStripes: {
    size: 160,
    body: star(30, 30, 9) + star(66, 56, 6) + star(26, 72, 5) + `<path d="M84 96H160M84 112H160M84 128H160" ${line}/>` + star(122, 42, 7) + `<path d="M0 118H56M0 134H56" ${line}/>`,
  },
  batsAndPumpkins: { size: 170, body: at(46, 42, `<path d="${bat}"/>`, 0, 1.35) + at(128, 118, pumpkin, 0, 1.1) + at(130, 38, `<path d="${bat}"/>`, -10, 0.8) + at(44, 126, `<path d="${bat}"/>`, 8, 1) },
  marigolds: { size: 160, body: at(44, 46, marigold) + at(116, 112, skull, 0, 1.05) + at(118, 36, marigold, 0, 0.55) + at(40, 122, marigold, 0, 0.7) },
  diyas: { size: 160, body: at(44, 44, rangoli, 0, 0.95) + at(118, 112, diya) + at(118, 40, diya, 0, 0.6) + at(36, 118, diya, 0, 0.75) },
  harvest: { size: 150, body: at(38, 46, wheat, 12) + at(112, 104, acorn) + at(110, 34, leaf, 40, 0.65) + at(36, 122, acorn, 0, 0.55) },
  menorahs: { size: 170, body: at(50, 48, menorah) + at(126, 116, davidStar) + at(128, 38, davidStar, 0, 0.55) + at(40, 130, davidStar, 0, 0.7) },
  snowAndTrees: { size: 160, body: at(44, 44, flake(19)) + at(116, 112, tree) + at(120, 36, flake(10)) + at(36, 122, flake(13)) + dot(78, 76, 1.6) },
} satisfies Record<string, PatternTile>;

export type PatternId = keyof typeof patternTiles;

/** The tile as a CSS image, in colour at the given opacity. */
export function patternImage(id: PatternId, color: string, opacity: number): string {
  const { size, body } = patternTiles[id];
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}" color="${color}"><g fill="currentColor" opacity="${opacity}">${body}</g></svg>`;
  return `url("data:image/svg+xml,${encodeURIComponent(svg).replace(/'/g, "%27")}")`;
}
