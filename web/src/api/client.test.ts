import { describe, expect, it } from "vitest";
import { endsSession } from "./client";

describe("endsSession", () => {
  it("signs out when the current token is refused", () => {
    expect(endsSession("Bearer abc", "abc", "unauthorized")).toBe(true);
    expect(endsSession("Bearer abc", "abc", undefined)).toBe(true);
  });

  it("keeps the session when the credentials just entered were wrong", () => {
    expect(endsSession("Bearer abc", "abc", "wrong_pin")).toBe(false);
    expect(endsSession("Bearer abc", "abc", "invalid_totp")).toBe(false);
  });

  it("ignores a late answer to a request sent with an earlier token (profile switch)", () => {
    expect(endsSession("Bearer old", "new", "unauthorized")).toBe(false);
    expect(endsSession("Bearer old", null, "unauthorized")).toBe(false);
  });
});
