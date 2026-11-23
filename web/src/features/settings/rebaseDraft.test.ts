import { describe, expect, it } from "vitest";
import { rebaseDraft } from "./rebaseDraft";

type Tasks = { windowStart: string; disabled: string[]; keepBackups: number; apiKey?: string; apiKeySet?: boolean };

const seen: Tasks = { windowStart: "02:00", disabled: [], keepBackups: 7 };

describe("rebaseDraft", () => {
  it("takes the server's data when nothing was being edited", () => {
    const server = { ...seen, disabled: ["scan"] };
    expect(rebaseDraft(seen, seen, server)).toEqual(server);
  });

  it("keeps an unsaved edit when another field changes on the server", () => {
    const draft = { ...seen, windowStart: "04:00" };
    const server = { ...seen, disabled: ["scan"] };
    expect(rebaseDraft(draft, seen, server)).toEqual({ windowStart: "04:00", disabled: ["scan"], keepBackups: 7 });
  });

  it("after its own save, keeps only what was typed while the save was on its way", () => {
    const sent: Tasks = { ...seen, windowStart: "04:00", apiKey: "secret" };
    const draft: Tasks = { ...sent, keepBackups: 14 }; // typed during the save
    const server: Tasks = { ...seen, windowStart: "04:00", apiKeySet: true }; // the key isn't sent back
    expect(rebaseDraft(draft, sent, server)).toEqual({ windowStart: "04:00", disabled: [], keepBackups: 14, apiKeySet: true });
  });

  it("keeps a field cleared in the draft", () => {
    const base: Tasks = { ...seen, apiKey: "old" };
    const draft: Tasks = { ...base, apiKey: undefined };
    expect(rebaseDraft(draft, base, base).apiKey).toBeUndefined();
  });

  it("is the server's data when there's no draft or base yet", () => {
    expect(rebaseDraft(undefined as unknown as Tasks, undefined, seen)).toBe(seen);
    expect(rebaseDraft(seen, undefined, seen)).toBe(seen);
  });
});
