"""What a track is like, from AudioSet's sound classes (MUSIC-18).

The Audio Spectrogram Transformer fine-tuned on AudioSet (MIT/ast-finetuned-audioset,
BSD-3) scores 527 classes, among them seven moods of music ("Happy music", "Sad music"…),
singing, dance and electronic styles and acoustic instruments. A trait is the strongest of
its classes, averaged over a track's excerpts. The scores are relative (a "sad" 0.2 can be
a very sad track), so the server compares them across the library rather than to a fixed
line.
"""

# Each trait and the AudioSet classes it takes the strongest of. Names absent from a model's
# labels are skipped.
TRAITS = {
    "happy": ["Happy music", "Funny music"],
    "sad": ["Sad music"],
    "tender": ["Tender music"],
    "exciting": ["Exciting music"],
    "angry": ["Angry music"],
    "scary": ["Scary music"],
    "dance": ["Dance music", "Disco", "House music", "Techno", "Electronic dance music",
              "Drum and bass", "Dubstep", "Trance music", "Hip hop music", "Funk"],
    "vocal": ["Singing", "Male singing", "Female singing", "Choir", "Rapping", "Vocal music",
              "A capella", "Child singing", "Chant", "Yodeling"],
    "electronic": ["Electronic music", "Synthesizer", "Electronica", "Techno", "House music",
                   "Ambient music", "Drum machine"],
    "acoustic": ["Acoustic guitar", "Piano", "Folk music", "Classical music", "Violin, fiddle",
                 "Cello", "Harp", "Strum", "Bluegrass", "Country"],
    "ambient": ["Ambient music", "New-age music", "Drone"],
}


def label_index(id2label):
    """Maps each trait to the indexes of its classes in a model's labels."""
    by_name = {name: int(i) for i, name in id2label.items()}
    return {t: [by_name[n] for n in names if n in by_name] for t, names in TRAITS.items()}


def group(probs, index):
    """Traits from one track's class probabilities (a sequence by class index): the strongest
    class of each, rounded. A trait without classes in the model is left out."""
    out = {}
    for t, idx in index.items():
        if idx:
            out[t] = round(float(max(probs[i] for i in idx)), 4)
    return out
