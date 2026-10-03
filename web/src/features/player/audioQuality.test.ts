import { describe, expect, it } from "vitest";
import { audioCodecLabel, audioQualityLabel, audioQualityShort, isHiRes, streamedLabel } from "./audioQuality";

const decision = (method: "direct_play" | "direct_stream" | "transcode", audioCopy = false) => ({
  method,
  summary: "",
  reasons: [],
  videoCopy: false,
  audioCopy,
  burnSubtitle: false,
  toneMap: false,
  audioCodec: "aac",
});

describe("audio quality labels", () => {
  it("labels codecs", () => {
    expect(["flac", "alac", "mp3", "aac", "opus", "vorbis", "pcm_s24le", "wavpack", "ape", "dsd_lsbf", "tta"].map(audioCodecLabel)).toEqual([
      "FLAC", "ALAC", "MP3", "AAC", "Opus", "Ogg Vorbis", "PCM", "WavPack", "APE", "DSD", "TTA",
    ]);
  });
  it("formats lossless and lossy", () => {
    expect(audioQualityLabel({ codec: "flac", lossless: true, bitDepth: 24, sampleRate: 96000 })).toBe("FLAC · 24-bit/96 kHz");
    expect(audioQualityLabel({ codec: "flac", lossless: true, sampleRate: 44100 })).toBe("FLAC · 44.1 kHz");
    expect(audioQualityLabel({ codec: "mp3", lossless: false, bitrateKbps: 320, sampleRate: 44100 })).toBe("MP3 · 320 kbps");
    expect(audioQualityShort({ codec: "flac", lossless: true, bitDepth: 24, sampleRate: 88200 })).toBe("FLAC 24/88.2");
    expect(audioQualityShort({ codec: "mp3", lossless: false, bitrateKbps: 320 })).toBe("MP3 320");
  });
  it("tags hi-res", () => {
    expect(isHiRes({ codec: "flac", lossless: true, bitDepth: 24, sampleRate: 44100 })).toBe(true);
    expect(isHiRes({ codec: "flac", lossless: true, bitDepth: 16, sampleRate: 48000 })).toBe(false);
    expect(isHiRes({ codec: "aac", lossless: false, sampleRate: 96000 })).toBe(false);
  });
  it("says what is streamed when it isn't the original", () => {
    expect(streamedLabel({ decision: decision("direct_play") })).toBe("");
    expect(streamedLabel({ decision: decision("direct_stream", true) })).toBe("");
    expect(streamedLabel({ decision: { ...decision("transcode"), audioKbps: 256 } })).toBe("AAC 256 kbps");
    expect(streamedLabel({ decision: { ...decision("transcode"), audioKbps: 128 }, limitKbps: 128 })).toBe("AAC 128 kbps");
  });
});
