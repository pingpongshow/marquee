"""Marquee Soundprint analysis service (MUSIC-1, D56).

Runs next to the Marquee server on the same host and is only reachable from it
(127.0.0.1). It reads audio files (read-only mounts) and returns:

- a CLAP embedding (LAION larger_clap_general, 512-d, unit length) that captures how a
  track sounds; text prompts embed into the same space, which powers Muse. A second,
  music-trained CLAP (larger_clap_music) can be asked for instead, so the server can try it
  on the library before switching (MUSIC-1);
- tempo (BPM), musical key, and an energy estimate (librosa);
- traits: how happy, sad, tender, exciting, angry, danceable, sung, electronic, acoustic and
  ambient a track sounds, from AudioSet's classes (MIT/ast-finetuned-audioset, see traits.py);
- text embeddings of movie and show descriptions (BAAI/bge-base-en-v1.5, 768-d, unit
  length) for Muse for movies, recommendations and related items (USER-15, USER-16).

Nothing leaves the machine except the one-time model download from Hugging Face.

The models load on first use and unload after IDLE_UNLOAD_MINUTES (default 5) without
requests, so the GPU is free while nothing is being analysed. /health never loads them.
"""

import logging
import os
import subprocess
import threading
from concurrent.futures import ThreadPoolExecutor

import librosa
import numpy as np
import torch
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from transformers import (ASTFeatureExtractor, ASTForAudioClassification, AutoModel, AutoTokenizer,
                          ClapConfig, ClapModel, ClapProcessor)

import traits as trait_classes
from idle import IdleModel, start_reaper


def env(name: str, default: str) -> str:
    """Reads SOUNDPRINT_<name>, falling back to the older SONIC_<name>."""
    return os.environ.get(f"SOUNDPRINT_{name}") or os.environ.get(f"SONIC_{name}") or default


MODEL_ID = env("MODEL", "laion/larger_clap_general")
MUSIC_MODEL_ID = env("MUSIC_MODEL", "laion/larger_clap_music")
# The sound models the server can ask for, by the name it uses.
MODELS = {"general": MODEL_ID, "music": MUSIC_MODEL_ID}
# Reported to the server as the analysis version: bumping the suffix re-analyses the library.
ANALYSIS_IDS = {k: f"{m}#2" for k, m in MODELS.items()}
ANALYSIS_ID = ANALYSIS_IDS["general"]
TRAITS_MODEL_ID = env("TRAITS_MODEL", "MIT/ast-finetuned-audioset-10-10-0.4593")
TRAITS_ID = f"{TRAITS_MODEL_ID}#1"
TRAITS_SR = 16_000  # AST's sample rate
SR = 48_000          # CLAP's sample rate
WINDOW = 10.0        # seconds per CLAP window
FFMPEG = os.environ.get("FFMPEG", "ffmpeg")
DOC_MODEL_ID = env("DOC_MODEL", "BAAI/bge-base-en-v1.5")
# bge wants this instruction on short queries (not on the documents they search).
DOC_QUERY_PREFIX = "Represent this sentence for searching relevant passages: "

log = logging.getLogger("soundprint")
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")

device = "cuda" if torch.cuda.is_available() else "cpu"
IDLE_SECONDS = float(os.environ.get("IDLE_UNLOAD_MINUTES", "5")) * 60
# A processor (tokenizer and audio features) runs on the CPU and stays once loaded; the
# config gives the embedding size without loading the model.
processors = {"general": ClapProcessor.from_pretrained(MODEL_ID)}
processors_mu = threading.Lock()
CLAP_DIMS = int(ClapConfig.from_pretrained(MODEL_ID).projection_dim)
gpu_lock = threading.Lock()


def processor_for(name: str) -> ClapProcessor:
    with processors_mu:
        if name not in processors:
            processors[name] = ClapProcessor.from_pretrained(MODELS[name])
        return processors[name]


def clap_loader(model_id: str):
    def load():
        m = ClapModel.from_pretrained(model_id).to(device).eval()
        return m.half() if device == "cuda" else m
    return load


claps = {k: IdleModel(f"{m} on {device}", clap_loader(m), IDLE_SECONDS) for k, m in MODELS.items()}
clap = claps["general"]


def model_name(name) -> str:
    """The sound model a request asks for ("general" when it doesn't say)."""
    name = name or "general"
    if name not in MODELS:
        raise HTTPException(400, f"model must be one of {', '.join(MODELS)}")
    return name
decoders = ThreadPoolExecutor(max_workers=int(env("DECODERS", "8")))

app = FastAPI(title="Marquee Soundprint analysis")

# Krumhansl–Schmuckler key profiles.
MAJOR = np.array([6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88])
MINOR = np.array([6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17])
NOTES = ["C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"]


def decode(path: str, start: float, seconds: float, sr: int) -> np.ndarray:
    """Decodes a mono float32 excerpt with FFmpeg (fast seek)."""
    cmd = [FFMPEG, "-v", "error", "-ss", f"{max(start, 0):.2f}", "-t", f"{seconds:.2f}", "-i", path,
           "-vn", "-ac", "1", "-ar", str(sr), "-f", "f32le", "pipe:1"]
    out = subprocess.run(cmd, capture_output=True, timeout=60)
    if out.returncode != 0:
        raise RuntimeError(out.stderr.decode(errors="replace").strip()[:300])
    return np.frombuffer(out.stdout, dtype=np.float32)


def duration(path: str) -> float:
    out = subprocess.run(["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path],
                         capture_output=True, timeout=30)
    try:
        return float(out.stdout.decode().strip())
    except ValueError:
        return 0.0


def excerpts(path: str):
    """Three 10 s windows across the track (or the whole track when short)."""
    d = duration(path)
    if d <= 0:
        raise RuntimeError("unreadable file")
    if d <= WINDOW * 3:
        whole = decode(path, 0, d, SR)
        n = int(WINDOW * SR)
        return d, [whole[i:i + n] for i in range(0, max(len(whole) - n // 2, 1), n)][:3] or [whole]
    return d, [decode(path, d * f - WINDOW / 2, WINDOW, SR) for f in (0.25, 0.5, 0.75)]


def features(path: str, d: float):
    """Tempo, key and energy from a 30 s excerpt around the middle."""
    y = decode(path, max(d / 2 - 15, 0), min(30, d), 22_050)
    if len(y) < 22_050 * 3:
        return None, None, None, None
    tempo, _ = librosa.beat.beat_track(y=y, sr=22_050)
    bpm = float(np.atleast_1d(tempo)[0])
    chroma = librosa.feature.chroma_cqt(y=y, sr=22_050).mean(axis=1)
    best = (-2.0, 0, "major")
    for i in range(12):
        for mode, prof in (("major", MAJOR), ("minor", MINOR)):
            c = np.corrcoef(chroma, np.roll(prof, i))[0, 1]
            if c > best[0]:
                best = (c, i, mode)
    return round(bpm, 1), NOTES[best[1]], best[2], energy(y, 22_050)


def energy(y: np.ndarray, sr: int) -> float:
    """0–1: how intense a track feels. Loudness alone barely varies across mastered music,
    so it's blended with rhythmic drive (onset strength) and brightness (spectral centroid)."""
    loud = np.clip((20 * np.log10(np.sqrt(np.mean(y ** 2)) + 1e-9) + 30) / 20, 0, 1)  # -30 dBFS → 0, -10 → 1
    drive = np.clip(librosa.onset.onset_strength(y=y, sr=sr).mean() / 1.8, 0, 1)
    bright = np.clip((librosa.feature.spectral_centroid(y=y, sr=sr).mean() - 800) / 2700, 0, 1)
    return round(float(0.35 * loud + 0.45 * drive + 0.2 * bright), 3)


def embed_audio(clips, name="general"):
    processor = processor_for(name)
    with gpu_lock, torch.inference_mode():
        model = claps[name].get()
        inputs = processor(audios=clips, sampling_rate=SR, return_tensors="pt")
        inputs = {k: v.to(device) for k, v in inputs.items()}
        if device == "cuda":
            inputs = {k: v.half() if v.dtype == torch.float32 else v for k, v in inputs.items()}
        e = model.get_audio_features(**inputs).float()
        e = torch.nn.functional.normalize(e, dim=-1)
    return e.cpu().numpy()


class AnalyzeRequest(BaseModel):
    paths: list[str]
    model: str | None = None  # "general" (default) or "music"
    features: bool = True     # tempo, key and energy too (a model being tried only needs embeddings)


class TextRequest(BaseModel):
    texts: list[str]
    model: str | None = None


class TraitsRequest(BaseModel):
    paths: list[str]


def analyze_one(path: str, with_features: bool = True):
    d, clips = excerpts(path)
    clips = [c for c in clips if len(c) >= SR]  # empty or sub-second decodes break the CLAP processor
    if not clips:
        raise RuntimeError("no decodable audio")
    return d, clips, features(path, d) if with_features else (None, None, None, None)


@app.get("/health")
def health():
    return {"model": ANALYSIS_ID, "models": ANALYSIS_IDS, "traits": TRAITS_ID, "device": device, "dims": CLAP_DIMS,
            "loaded": {"clap": clap.loaded, "music": claps["music"].loaded, "traits": ast.loaded, "docs": docs.loaded}}


@app.post("/analyze")
def analyze(req: AnalyzeRequest):
    if len(req.paths) > 64:
        raise HTTPException(400, "at most 64 paths per request")
    name = model_name(req.model)
    jobs = {p: decoders.submit(analyze_one, p, req.features) for p in req.paths}
    results, clips, owners = {}, [], []
    for p, job in jobs.items():
        try:
            d, cs, (bpm, key, mode, energy) = job.result()
            results[p] = {"path": p, "duration": d, "bpm": bpm, "key": key, "mode": mode, "energy": energy}
            clips += cs
            owners += [p] * len(cs)
        except Exception as e:  # noqa: BLE001 — report per file, keep the batch going
            results[p] = {"path": p, "error": str(e)}
    if clips:
        try:
            emb = embed_audio(clips, name)
        except Exception:  # noqa: BLE001 — find the bad file instead of failing the whole batch
            log.exception("batch embedding failed; retrying per file")
            emb = np.zeros((len(clips), CLAP_DIMS), dtype=np.float32)
            for p in set(owners):
                idx = [i for i, o in enumerate(owners) if o == p]
                try:
                    emb[idx] = embed_audio([clips[i] for i in idx], name)
                except Exception as e:  # noqa: BLE001
                    results[p] = {"path": p, "error": f"embedding failed: {e}"[:300]}
                    owners = [o if o != p else None for o in owners]
        for p in set(o for o in owners if o):
            idx = [i for i, o in enumerate(owners) if o == p]
            v = emb[idx].mean(axis=0)
            v /= np.linalg.norm(v) + 1e-9
            results[p]["embedding"] = [round(float(x), 6) for x in v]
    return {"model": ANALYSIS_IDS[name], "results": [results[p] for p in req.paths]}


@app.post("/embed_text")
def embed_text(req: TextRequest):
    if not req.texts or len(req.texts) > 32:
        raise HTTPException(400, "1–32 texts")
    name = model_name(req.model)
    processor = processor_for(name)
    with gpu_lock, torch.inference_mode():
        model = claps[name].get()
        inputs = processor(text=req.texts, return_tensors="pt", padding=True)
        inputs = {k: v.to(device) for k, v in inputs.items()}
        e = torch.nn.functional.normalize(model.get_text_features(**inputs).float(), dim=-1)
    return {"model": ANALYSIS_IDS[name], "embeddings": [[round(float(x), 6) for x in v] for v in e.cpu().numpy()]}


def load_ast():
    extractor = ASTFeatureExtractor.from_pretrained(TRAITS_MODEL_ID)
    m = ASTForAudioClassification.from_pretrained(TRAITS_MODEL_ID).to(device).eval()
    return (m.half() if device == "cuda" else m), extractor, trait_classes.label_index(m.config.id2label)


ast = IdleModel(f"{TRAITS_MODEL_ID} on {device}", load_ast, IDLE_SECONDS)


def trait_clips(path: str):
    """Three 10 s windows at AST's rate (the whole track when short)."""
    d = duration(path)
    if d <= 0:
        raise RuntimeError("unreadable file")
    if d <= WINDOW * 3:
        return [decode(path, 0, min(d, WINDOW), TRAITS_SR)]
    return [decode(path, d * f - WINDOW / 2, WINDOW, TRAITS_SR) for f in (0.25, 0.5, 0.75)]


@app.post("/traits")
def traits(req: TraitsRequest):
    """Traits of up to 64 files (see traits.py), averaged over three excerpts each."""
    if len(req.paths) > 64:
        raise HTTPException(400, "at most 64 paths per request")
    jobs = {p: decoders.submit(trait_clips, p) for p in req.paths}
    results = {}
    for p, job in jobs.items():
        try:
            clips = [c for c in job.result() if len(c) >= TRAITS_SR]
            if not clips:
                raise RuntimeError("no decodable audio")
            with gpu_lock, torch.inference_mode():
                m, extractor, index = ast.get()
                inputs = extractor(clips, sampling_rate=TRAITS_SR, return_tensors="pt")
                inputs = {k: v.to(device) for k, v in inputs.items()}
                if device == "cuda":
                    inputs = {k: v.half() if v.dtype == torch.float32 else v for k, v in inputs.items()}
                probs = torch.sigmoid(m(**inputs).logits.float()).mean(dim=0).cpu().numpy()
            results[p] = {"path": p, "traits": trait_classes.group(probs, index)}
        except Exception as e:  # noqa: BLE001 — report per file, keep the batch going
            results[p] = {"path": p, "error": str(e)[:300]}
    return {"model": TRAITS_ID, "results": [results[p] for p in req.paths]}


class DocsRequest(BaseModel):
    texts: list[str]
    kind: str = "doc"  # "query" (a Muse prompt) or "doc" (a movie's or show's description)


def load_doc_model():
    """Loads the text model on first use, so a music-only server (or one using Ollama for
    movies) never pays for it."""
    tok = AutoTokenizer.from_pretrained(DOC_MODEL_ID)
    m = AutoModel.from_pretrained(DOC_MODEL_ID).to(device).eval()
    return (m.half() if device == "cuda" else m), tok


docs = IdleModel(f"{DOC_MODEL_ID} on {device}", load_doc_model, IDLE_SECONDS)
start_reaper([*claps.values(), ast, docs], gpu_lock)


@app.post("/embed_docs")
def embed_docs(req: DocsRequest):
    """Embeds descriptions or queries with bge (CLS pooling, unit length). An empty list
    just reports the model id, which the server stores so a model change re-embeds."""
    if len(req.texts) > 64:
        raise HTTPException(400, "at most 64 texts per request")
    if req.kind not in ("query", "doc"):
        raise HTTPException(400, 'kind must be "query" or "doc"')
    if not req.texts:
        return {"model": DOC_MODEL_ID, "embeddings": []}
    texts = [DOC_QUERY_PREFIX + t if req.kind == "query" else t for t in req.texts]
    with gpu_lock, torch.inference_mode():
        m, tok = docs.get()
        inputs = tok(texts, padding=True, truncation=True, max_length=512, return_tensors="pt")
        inputs = {k: v.to(device) for k, v in inputs.items()}
        e = m(**inputs).last_hidden_state[:, 0].float()
        e = torch.nn.functional.normalize(e, dim=-1)
    return {"model": DOC_MODEL_ID, "embeddings": [[round(float(x), 6) for x in v] for v in e.cpu().numpy()]}
