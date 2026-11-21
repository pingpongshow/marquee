"""Which language a film's or episode's audio track is spoken in, for tracks whose file
doesn't say (Whisper's language identification).

Three short stretches spread across the track are heard (not the whole track), the quiet ones
(silence, a title card) and those without speech (music, effects) left out. Each gives
Whisper's odds for every language. A language counts only when at least two stretches agree
on it, so one scene in another language doesn't decide the whole track; the server also wants
the stretches' averaged odds to be clear.
"""

import numpy as np

SR = 16_000        # Whisper's sample rate
CLIP = 20.0        # seconds per stretch (Whisper hears up to 30 s at once)
SPOTS = (0.25, 0.5, 0.75)  # where in the track, as shares of its length
MIN_RMS = 0.01     # quieter than this (about -40 dBFS) isn't worth hearing
# Whisper's odds that a stretch has no speech in it (music, effects, a tone): above this it's
# left out, or a music-only track would get a language. Speech scores under 0.01, music
# from about 0.6.
NO_SPEECH = 0.3
AGREE = 2          # stretches that must agree on the language


def spots(duration: float):
    """Start times of the stretches heard; a track too short for three gets fewer."""
    if duration <= CLIP:
        return [0.0]
    if duration <= CLIP * len(SPOTS):
        return [i * CLIP for i in range(int(duration // CLIP))]
    return [max(0.0, duration * f - CLIP / 2) for f in SPOTS]


def loud_enough(clip: np.ndarray) -> bool:
    if len(clip) < SR * 2:
        return False
    return float(np.sqrt(np.mean(np.square(clip, dtype=np.float64)))) >= MIN_RMS


def combine(odds: list[dict[str, float]]):
    """The stretches' verdict: (language, its averaged share of the odds, stretches heard,
    stretches whose own most likely language it is). No language unless AGREE of them agree."""
    if not odds:
        return None, 0.0, 0, 0
    votes: dict[str, int] = {}
    total: dict[str, float] = {}
    for o in odds:
        top = max(o, key=o.get)
        votes[top] = votes.get(top, 0) + 1
        for lang, p in o.items():
            total[lang] = total.get(lang, 0.0) + p
    lang = max(total, key=total.get)
    agree = votes.get(lang, 0)
    if agree < AGREE:
        return None, total[lang] / len(odds), len(odds), agree
    return lang, total[lang] / len(odds), len(odds), agree
