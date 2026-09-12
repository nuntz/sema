import { afterEach, describe, expect, it, vi } from "vitest";
import { HOUR, LIFETIME } from "./expiry";
import { readerDay, readerDeadlineLine } from "./reader-expiry";

afterEach(() => vi.unstubAllEnvs());
const now = Date.parse("2026-09-08T02:00:00Z");
const published = (deadline: string) =>
  new Date(Date.parse(deadline) - LIFETIME).toISOString();
describe("reader lifetime", () => {
  it("tracks the current day of seven from publication", () => {
    for (const [hours, day] of [
      [0, 1],
      [23.99, 1],
      [24, 2],
      [120, 6],
      [144, 7],
      [180, 7],
    ] as const)
      expect(readerDay(new Date(now - hours * HOUR).toISOString(), now)).toBe(
        day,
      );
  });
  it("uses actual local deadlines and reader wording", () => {
    vi.stubEnv("TZ", "America/Vancouver");
    expect(readerDeadlineLine(published("2026-09-08T05:00:00Z"), now)).toBe(
      "Goes at 22:00 tonight — about three hours.",
    );
    expect(readerDeadlineLine(published("2026-09-08T16:14:00Z"), now)).toBe(
      "Goes tomorrow at 09:14 unless you keep it.",
    );
    expect(readerDeadlineLine(published("2026-09-10T16:14:00Z"), now)).toBe(
      "Goes Thursday at 09:14 unless you keep it.",
    );
  });
  it("does not call an urgent deadline after midnight tonight", () => {
    vi.stubEnv("TZ", "America/Vancouver");
    expect(
      readerDeadlineLine(
        published("2026-09-08T08:00:00Z"),
        Date.parse("2026-09-08T05:00:00Z"),
      ),
    ).toBe("Goes at 01:00 tomorrow — about three hours.");
  });
  it("keeps elapsed items truthful while waiting for a refresh", () => {
    expect(readerDeadlineLine(published("2026-09-08T01:00:00Z"), now)).toBe(
      "Goes now unless you keep it.",
    );
  });
});
