import { describe, expect, it } from "vitest";
import { session } from "@/api/client";
import { originalDownloadUrl, zipDownloadUrl } from "./Download";

describe("download links", () => {
  it("carry the session token and an optional version", () => {
    session.set("a b");
    expect(originalDownloadUrl(42)).toBe("/api/v1/download/original/42?token=a%20b");
    expect(originalDownloadUrl(42, 7)).toBe("/api/v1/download/original/42?fileId=7&token=a%20b");
    expect(zipDownloadUrl(9)).toBe("/api/v1/download/zip/9?token=a%20b");
    session.set(null);
  });
});
