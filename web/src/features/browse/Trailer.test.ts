import { describe, expect, it } from "vitest";
import { youtubeEmbedUrl } from "./Trailer";

describe("youtubeEmbedUrl", () => {
  it("embeds from youtube-nocookie with autoplay and no related videos", () => {
    expect(youtubeEmbedUrl("dQw4w9WgXcQ")).toBe("https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?autoplay=1&rel=0");
  });
  it("escapes the key", () => {
    expect(youtubeEmbedUrl("a/b?c")).toBe("https://www.youtube-nocookie.com/embed/a%2Fb%3Fc?autoplay=1&rel=0");
  });
});
