import { describe, expect, it } from "vitest";
import { audioTrackLabel, channelsLabel, resolutionLabel, splitPath, subtitleLanguages, versionLabel } from "./format";

describe("file details", () => {
  it("names resolutions", () => {
    expect(resolutionLabel({ width: 3840, height: 2160 })).toBe("4K");
    expect(resolutionLabel({ width: 1920, height: 800 })).toBe("1080p");
    expect(resolutionLabel({ width: 1280, height: 720 })).toBe("720p");
    expect(resolutionLabel({ width: 720, height: 480 })).toBe("SD");
    expect(resolutionLabel({})).toBe("");
  });
  it("names channel layouts", () => {
    expect(channelsLabel(2)).toBe("2.0");
    expect(channelsLabel(6)).toBe("5.1");
    expect(channelsLabel(8)).toBe("7.1");
    expect(channelsLabel(1)).toBe("Mono");
    expect(channelsLabel(undefined)).toBe("");
  });
  it("splits paths into name and folder", () => {
    expect(splitPath("/media/movies/Heat (1995)/Heat (1995).mkv")).toEqual({ name: "Heat (1995).mkv", folder: "/media/movies/Heat (1995)" });
    expect(splitPath("file.mkv")).toEqual({ name: "file.mkv", folder: "" });
  });
  it("labels versions as before", () => {
    expect(versionLabel({ label: "Extended", files: [{ width: 3840, height: 2160, hdrFormat: "dolby_vision", videoCodec: "hevc" }] })).toBe("Extended (4K · Dolby Vision · HEVC)");
  });
});

describe("tracks on a title's page", () => {
  it("names audio tracks", () => {
    expect(audioTrackLabel({ language: "eng", codec: "truehd", channels: 8 })).toBe("English · Dolby TrueHD 7.1");
    expect(audioTrackLabel({ language: "eng", codec: "ac3", channels: 2, title: "Director's Commentary" })).toBe("English · Dolby Digital Stereo · Director's Commentary");
    expect(audioTrackLabel({ language: "fre", codec: "dts", profile: "DTS-HD MA", channels: 6, title: "French 5.1" })).toBe("French · DTS-HD MA 5.1");
    expect(audioTrackLabel({ language: "jpn", codec: "pcm_s16le", channels: 6, title: "Surround" })).toBe("Japanese · PCM 5.1");
    expect(audioTrackLabel({ language: "eng", codec: "eac3", profile: "Dolby Digital Plus + Dolby Atmos", channels: 6, title: "MiNX - Small HD episodes" })).toBe(
      "English · Dolby Digital Plus Atmos 5.1",
    );
    expect(audioTrackLabel({ language: "por", codec: "aac", channels: 2, title: "Brazilian" })).toBe("Portuguese · AAC Stereo · Brazilian");
    expect(audioTrackLabel({ codec: "aac" })).toBe("Unknown · AAC");
  });
  it("lists subtitle languages once each", () => {
    const sub = (language: string | undefined, forced = false, hearingImpaired = false) => ({ kind: "subtitle", language, forced, hearingImpaired });
    expect(
      subtitleLanguages([{ kind: "audio", language: "eng", forced: false, hearingImpaired: false }, sub("eng"), sub("eng"), sub("eng", false, true), sub("fre", true), sub("und")]),
    ).toEqual(["English", "English (SDH)", "French (Forced)", "Unknown"]);
    expect(subtitleLanguages([])).toEqual([]);
  });
});
