import { describe, expect, it } from "vitest";
import { describeTimeLimits, formatAllowance, formatMinutes, parseAllowance } from "./timeLimits";

describe("parseAllowance", () => {
  it("reads minutes and hours", () => {
    expect(parseAllowance("90")).toBe(90);
    expect(parseAllowance(" 45 min ")).toBe(45);
    expect(parseAllowance("1:30")).toBe(90);
    expect(parseAllowance("1h30")).toBe(90);
    expect(parseAllowance("2h")).toBe(120);
    expect(parseAllowance("24:00")).toBe(1440);
  });
  it("treats empty as no limit and refuses the rest", () => {
    expect(parseAllowance("")).toBeNull();
    expect(parseAllowance("0")).toBeUndefined();
    expect(parseAllowance("1:75")).toBeUndefined();
    expect(parseAllowance("25:00")).toBeUndefined();
    expect(parseAllowance("lots")).toBeUndefined();
  });
});

describe("formatting", () => {
  it("formats allowances and minutes", () => {
    expect(formatAllowance(45)).toBe("45 min");
    expect(formatAllowance(90)).toBe("1:30");
    expect(formatMinutes(135)).toBe("2 h 15 min");
    expect(formatMinutes(120)).toBe("2 h");
    expect(formatMinutes(5)).toBe("5 min");
    expect(parseAllowance(formatAllowance(95))).toBe(95);
  });
  it("describes limits for the users list", () => {
    expect(describeTimeLimits({})).toBe("");
    expect(describeTimeLimits({ dailyLimitMinutes: 60, weekendLimitMinutes: 120 })).toBe("1 h a day, 2 h at weekends");
    expect(describeTimeLimits({ dailyLimitMinutes: 60, weekendLimitMinutes: 0, allowedFrom: "07:00", allowedUntil: "20:00" })).toBe(
      "1 h a day, no limit at weekends, 07:00–20:00",
    );
  });
});
