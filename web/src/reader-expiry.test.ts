import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HOUR, LIFETIME } from "./expiry";
import {
  deadlineGroup,
  deadlineTime,
  nextMidnight,
  readerDay,
  readerDeadlineLine,
} from "./reader-expiry";

const nativeTime = Date.prototype.toLocaleTimeString;
beforeEach(() => {
  vi.spyOn(Date.prototype, "toLocaleTimeString").mockImplementation(function (
    this: Date,
    optionsLocale,
    options,
  ) {
    return nativeTime.call(this, optionsLocale ?? "de-DE", options);
  });
});

it.each([
  ["en-US", "11:00 PM"],
  ["de-DE", "23:00"],
])("formats deadlines using %s", (locale, expected) => {
  vi.stubEnv("TZ", "America/Vancouver");
  vi.mocked(Date.prototype.toLocaleTimeString).mockImplementation(function (
    this: Date,
    _locales,
    options,
  ) {
    return nativeTime.call(this, locale, options);
  });
  expect(deadlineTime(published("2026-09-08T06:00:00Z"))).toBe(expected);
});
afterEach(() => {
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
});
const now = Date.parse("2026-09-08T02:00:00Z");
const published = (deadline: string) =>
  new Date(Date.parse(deadline) - LIFETIME).toISOString();
describe("reader lifetime", () => {
  it("uses local midnight boundaries for deadline wording", () => {
    vi.stubEnv("TZ", "America/Vancouver");
    expect(deadlineGroup(published("2026-09-08T06:59:59Z"), now)).toBe(
      "tonight",
    );
    expect(deadlineGroup(published("2026-09-08T07:00:00Z"), now)).toBe(
      "tomorrow",
    );
    expect(deadlineGroup(published("2026-09-09T07:00:00Z"), now)).toBe("later");
  });
  it("uses calendar midnight across daylight saving changes", () => {
    vi.stubEnv("TZ", "America/Vancouver");
    expect(nextMidnight(Date.parse("2026-03-08T09:00:00Z")).toISOString()).toBe(
      "2026-03-09T07:00:00.000Z",
    );
  });
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
      "Goes tomorrow at 9:14 unless you keep it.",
    );
    expect(readerDeadlineLine(published("2026-09-10T16:14:00Z"), now)).toBe(
      "Goes Thursday at 9:14 unless you keep it.",
    );
  });
  it("does not call an urgent deadline after midnight tonight", () => {
    vi.stubEnv("TZ", "America/Vancouver");
    expect(
      readerDeadlineLine(
        published("2026-09-08T08:00:00Z"),
        Date.parse("2026-09-08T05:00:00Z"),
      ),
    ).toBe("Goes at 1:00 tomorrow — about three hours.");
  });
  it("keeps elapsed items truthful while waiting for a refresh", () => {
    expect(readerDeadlineLine(published("2026-09-08T01:00:00Z"), now)).toBe(
      "Goes now unless you keep it.",
    );
  });
});
