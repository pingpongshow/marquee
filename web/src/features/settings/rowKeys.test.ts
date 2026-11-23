import { describe, expect, it } from "vitest";
import { rowKey, sameRow } from "./rowKeys";

describe("rowKey", () => {
  it("is the id once a row is saved", () => {
    expect(rowKey({ id: "abc" })).toBe("abc");
    expect(rowKey({ id: 7 })).toBe("7");
  });

  it("stays with an unsaved row through edits and other rows being removed", () => {
    type Row = { id?: string; name: string };
    const a: Row = { name: "a" };
    const b: Row = { name: "b" };
    const ka = rowKey(a);
    const kb = rowKey(b);
    expect(ka).not.toBe(kb);
    expect(rowKey(a)).toBe(ka);
    // Edited: a copy, the same row.
    const b2: Row = sameRow(b, { ...b, name: "b2" });
    expect(rowKey(b2)).toBe(kb);
    // With a removed, b is first in the list and still itself.
    expect([b2].map(rowKey)).toEqual([kb]);
  });
});
