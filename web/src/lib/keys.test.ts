import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ignoreShortcut } from "./keys";

class FakeElement {
  isContentEditable = false;
  constructor(public tagName: string) {}
  closest() {
    return null;
  }
}

const key = (over: Partial<KeyboardEvent> & { target?: unknown } = {}) =>
  ({ defaultPrevented: false, metaKey: false, ctrlKey: false, altKey: false, target: null, ...over }) as KeyboardEvent;

describe("ignoreShortcut", () => {
  let open: unknown = null;
  beforeEach(() => {
    open = null;
    vi.stubGlobal("HTMLElement", FakeElement);
    vi.stubGlobal("document", { body: {}, querySelector: () => open });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("leaves keys held with a modifier alone, unless the shortcut has one", () => {
    expect(ignoreShortcut(key())).toBe(false);
    expect(ignoreShortcut(key({ metaKey: true }))).toBe(true);
    expect(ignoreShortcut(key({ ctrlKey: true }))).toBe(true);
    expect(ignoreShortcut(key({ metaKey: true }), true)).toBe(false);
    expect(ignoreShortcut(key({ ctrlKey: true }), true)).toBe(false);
  });

  it("leaves text fields and open dialogs their keys, modifier or not", () => {
    const input = new FakeElement("INPUT");
    expect(ignoreShortcut(key({ target: input as unknown as EventTarget }))).toBe(true);
    expect(ignoreShortcut(key({ target: input as unknown as EventTarget, metaKey: true }), true)).toBe(true);
    expect(ignoreShortcut(key({ target: new FakeElement("DIV") as unknown as EventTarget }), true)).toBe(false);
    open = {};
    expect(ignoreShortcut(key({ ctrlKey: true }), true)).toBe(true);
    expect(ignoreShortcut(key({ defaultPrevented: true }), true)).toBe(true);
  });
});
