import type { ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { StorageReportView, formatBytes } from "./StorageCard";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params, hash }: { children: ReactNode; to: string; params?: Record<string, string>; hash?: string }) => {
    const href = Object.entries(params ?? {}).reduce((p, [k, v]) => p.replace(`$${k}`, v), to) + (hash ? `#${hash}` : "");
    return <a href={href}>{children}</a>;
  },
}));

const report = {
  volumes: [
    {
      path: "/config",
      label: "Config, Transcode",
      totalBytes: 500e9,
      freeBytes: 250e9,
    },
    {
      path: "/media/movies",
      label: "Movies, TV",
      totalBytes: 20e12,
      freeBytes: 1e12,
    },
  ],
  libraries: [
    {
      id: 1,
      name: "Movies",
      type: "movies" as const,
      bytes: 12e12,
      files: 1500,
      history: [
        { date: "2026-10-01", bytes: 11e12 },
        { date: "2026-10-18", bytes: 12e12 },
      ],
    },
    {
      id: 2,
      name: "TV",
      type: "shows" as const,
      bytes: 6e12,
      files: 9000,
      history: [{ date: "2026-10-18", bytes: 6e12 }],
    },
  ],
  largest: [
    {
      itemId: 7,
      title: "Lawrence of Arabia",
      type: "movie",
      year: 1962,
      libraryId: 1,
      bytes: 90e9,
      files: 1,
    },
    {
      itemId: 9,
      title: "Lost",
      type: "show",
      year: 2004,
      libraryId: 2,
      bytes: 400e9,
      files: 121,
    },
  ],
};

describe("StorageReportView", () => {
  it("shows the disks, libraries, growth and biggest titles", () => {
    const html = renderToStaticMarkup(<StorageReportView report={report} />);
    expect(html.match(/data-testid="storage-volume"/g)?.length).toBe(2);
    expect(html).toContain("250.0 GB free of 500.0 GB (50%)");
    expect(html).toContain("1.00 TB free of 20.00 TB (5%)");
    expect(html).toMatch(/text-danger[^>]*>1\.00 TB free/); // under 10% free is red
    expect(html.match(/data-testid="storage-library"/g)?.length).toBe(2);
    expect(html).toContain("+1.00 TB"); // Movies grew
    expect(html).toContain("No change"); // TV has one reading
    expect(html).toContain('data-testid="storage-growth"');
    expect(html.match(/<path/g)?.length).toBe(2);
    expect(html).toContain('href="/item/7"');
    // The 90 GB movie gets the Large files hint; the show's episodes are small.
    expect(html.match(/href="\/settings\/library-health#large-files"/g)?.length).toBe(1);
  });

  it("explains an empty growth chart", () => {
    const html = renderToStaticMarkup(<StorageReportView report={{ volumes: [], libraries: [], largest: [] }} />);
    expect(html).toContain('data-testid="storage-growth-empty"');
    expect(html).toContain("No libraries yet.");
  });

  it("formats sizes in decimal units", () => {
    expect(formatBytes(1500)).toBe("2 KB");
    expect(formatBytes(2.5e9)).toBe("2.5 GB");
    expect(formatBytes(3e12)).toBe("3.00 TB");
  });
});
