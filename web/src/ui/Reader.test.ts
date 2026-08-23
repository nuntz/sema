import { describe, expect, it } from "vitest";
import { isOlderThanThirtyDays } from "../archive";

describe("archive reader", () => {
  it("shows the stale-original note only after thirty days", () => {
    const now = Date.parse("2026-08-22T12:00:00Z");
    expect(isOlderThanThirtyDays("2026-07-22T11:59:59Z", now)).toBe(true);
    expect(isOlderThanThirtyDays("2026-07-23T12:00:00Z", now)).toBe(false);
  });
});
