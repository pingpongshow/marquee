import numpy as np

import speech


def test_three_short_stretches():
    assert speech.spots(15) == [0.0]
    assert speech.spots(50) == [0.0, 20.0]
    long = speech.spots(3600)
    assert len(long) == 3 and all(0 <= t <= 3600 - speech.CLIP for t in long)


def test_quiet_stretches_are_left_out():
    sr = speech.SR
    assert not speech.loud_enough(np.zeros(sr * 20, dtype=np.float32))
    assert not speech.loud_enough(np.full(sr, 0.5, dtype=np.float32))  # too short
    assert speech.loud_enough((0.1 * np.sin(np.arange(sr * 20) / 10)).astype(np.float32))


def test_stretches_must_agree():
    lang, share, heard, agree = speech.combine([{"en": 0.9, "de": 0.1}, {"en": 0.7, "nl": 0.3}, {"de": 0.8, "en": 0.2}])
    assert lang == "en" and heard == 3 and agree == 2 and abs(share - 0.6) < 1e-9
    # One stretch alone (the rest music) doesn't decide it, nor two that disagree.
    assert speech.combine([{"fr": 0.99}])[0] is None
    assert speech.combine([{"fr": 0.95, "en": 0.05}, {"en": 0.9, "fr": 0.1}])[0] is None
    assert speech.combine([]) == (None, 0.0, 0, 0)
