import { describe, expect, it } from "vitest";
import { portForSecurity } from "./smtp";

describe("portForSecurity", () => {
  it("follows the usual port", () => {
    expect(portForSecurity("tls", 587)).toBe(465);
    expect(portForSecurity("starttls", 465)).toBe(587);
    expect(portForSecurity("none", 587)).toBe(25);
    expect(portForSecurity("starttls", undefined)).toBe(587);
    expect(portForSecurity("tls", 0)).toBe(465);
  });
  it("keeps a port chosen on purpose", () => {
    expect(portForSecurity("tls", 2525)).toBe(2525);
    expect(portForSecurity("none", 1025)).toBe(1025);
  });
});
