/** Ten-band graphic equaliser for music, built from Web Audio biquad filters. */

export const EQ_BANDS = [31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000];
export const EQ_MAX_DB = 12;

/** Band gains in dB, lowest band first. */
export const EQ_PRESETS: Record<string, number[]> = {
  Flat: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
  "Bass Boost": [6, 5, 4, 2.5, 1, 0, 0, 0, 0, 0],
  "Bass Reducer": [-6, -5, -4, -2.5, -1, 0, 0, 0, 0, 0],
  "Treble Boost": [0, 0, 0, 0, 0, 1, 2.5, 4, 5, 6],
  Vocal: [-2, -3, -3, 1.5, 3.5, 3.5, 3, 1.5, 0, -1.5],
  Rock: [5, 4, 3, 1.5, -0.5, -1, 0.5, 2.5, 3.5, 4.5],
  Pop: [-1.5, -1, 0, 2, 4, 4, 2, 0, -1, -1.5],
  Jazz: [4, 3, 1.5, 2, -1.5, -1.5, 0, 1.5, 3, 3.5],
  Classical: [4.5, 3.5, 3, 2.5, -1.5, -1.5, 0, 2, 3, 3.5],
  Electronic: [4.5, 4, 1, 0, -2, 2, 1, 1, 4, 5],
  Loudness: [6, 4, 0, 0, -2, 0, -1, -5, 5, 1],
};

export type EqSettings = {
  enabled: boolean;
  /** A preset name, or "Custom" once a band has been moved by hand. */
  preset: string;
  gains: number[];
};

const KEY = "marquee.eq";
export const defaultEq: EqSettings = {
  enabled: false,
  preset: "Flat",
  gains: EQ_PRESETS.Flat!,
};

/** The equaliser is stored per device (browser), like volume. */
export function storedEq(): EqSettings {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? "null") as EqSettings | null;
    if (
      v &&
      typeof v.enabled === "boolean" &&
      typeof v.preset === "string" &&
      Array.isArray(v.gains) &&
      v.gains.length === EQ_BANDS.length &&
      v.gains.every((g) => typeof g === "number" && Math.abs(g) <= EQ_MAX_DB)
    )
      return v;
  } catch {
    /* storage unavailable or corrupt */
  }
  return defaultEq;
}

export function storeEq(eq: EqSettings) {
  try {
    localStorage.setItem(KEY, JSON.stringify(eq));
  } catch {
    /* storage unavailable */
  }
}

/** The filter chain: preamp → low shelf → 8 peaking bands → high shelf. */
export type EqChain = { input: GainNode; filters: BiquadFilterNode[] };

export function createEqChain(ctx: AudioContext): EqChain {
  const input = ctx.createGain();
  let prev: AudioNode = input;
  const filters = EQ_BANDS.map((freq, i) => {
    const f = ctx.createBiquadFilter();
    f.type =
      i === 0 ? "lowshelf" : i === EQ_BANDS.length - 1 ? "highshelf" : "peaking";
    f.frequency.value = freq;
    f.Q.value = 1.1; // about an octave wide, so neighbouring bands blend
    prev.connect(f);
    prev = f;
    return f;
  });
  return { input, filters };
}

/**
 * Routes `from` to `to` through the chain when on, else directly, and sets the band gains.
 * The preamp drops by half the largest boost so boosted music doesn't clip.
 */
export function routeEq(
  chain: EqChain,
  from: AudioNode,
  to: AudioNode,
  eq: EqSettings,
) {
  from.disconnect();
  chain.filters.at(-1)!.disconnect();
  if (!eq.enabled) {
    from.connect(to);
    return;
  }
  eq.gains.forEach((g, i) => (chain.filters[i]!.gain.value = g));
  const boost = Math.max(0, ...eq.gains);
  chain.input.gain.value = Math.pow(10, -boost / 2 / 20);
  from.connect(chain.input);
  chain.filters.at(-1)!.connect(to);
}
