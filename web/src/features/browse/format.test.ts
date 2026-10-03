import { describe, expect, it } from "vitest";
import { channelsLabel, resolutionLabel, splitPath, versionLabel } from "./format";

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
