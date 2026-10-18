import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ResourcesCard } from "./ServerStats";

const now = Date.parse("2026-10-18T12:00:00Z");
const status = {
  version: "1.0.0",
  startedAt: "2026-10-18T10:00:00Z",
  encoders: [],
  activeStreams: 0,
  activeTranscodes: 0,
  maxTranscodes: 4,
  localKbps: 0,
  remoteKbps: 0,
  libraries: [],
  host: { cpuCores: 32, memTotalBytes: 64e9, gpuName: "NVIDIA GPU" },
  resources: Array.from({ length: 30 }, (_, i) => ({
    at: new Date(now - (29 - i) * 10_000).toISOString(),
    hostCpu: 40,
    serverCpu: 12.4,
    hostMemBytes: 30e9,
    serverMemBytes: 2e9,
    gpu: 50,
    gpuEncoder: 40,
    gpuDecoder: 10,
  })),
};

describe("ResourcesCard", () => {
  it("draws CPU, memory and GPU lines with the latest readings", () => {
    const html = renderToStaticMarkup(<ResourcesCard status={status} />);
    expect(html.match(/<polyline/g)?.length).toBe(7); // System + Marquee twice, GPU + encoder + decoder
    expect(html).toContain("32 CPU threads");
    expect(html).toContain("NVIDIA GPU");
    expect(html).toContain("12%"); // Marquee's CPU now
    expect(html).toContain("2.0 GB"); // Marquee's memory now
  });

  it("explains when there are no readings", () => {
    const html = renderToStaticMarkup(<ResourcesCard status={{ ...status, host: undefined, resources: [] }} />);
    expect(html).toContain("CPU and memory readings aren");
  });
});
