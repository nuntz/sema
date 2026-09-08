import { afterEach, describe, expect, it, vi } from "vitest";
import { itemViewWindow } from "./item-view";

afterEach(() => vi.unstubAllEnvs());

describe("item view calendar windows", () => {
  it("keeps unread and all on the existing retention window", () => {
    expect(itemViewWindow("unread")).toBeUndefined();
    expect(itemViewWindow("all")).toBeUndefined();
  });

  it("uses local dates even when UTC has reached the next day", () => {
    vi.stubEnv("TZ", "America/Vancouver");
    const now = new Date("2026-09-08T02:00:00Z");
    expect(itemViewWindow("today", now)).toEqual({
      from: "2026-09-07T07:00:00.000Z",
      before: "2026-09-08T07:00:00.000Z",
    });
    expect(itemViewWindow("yesterday", now)).toEqual({
      from: "2026-09-06T07:00:00.000Z",
      before: "2026-09-07T07:00:00.000Z",
    });
  });

  it.each([
    [
      "2026-03-09T12:00:00Z",
      "2026-03-08T08:00:00.000Z",
      "2026-03-09T07:00:00.000Z",
    ],
    [
      "2026-11-02T12:00:00Z",
      "2026-11-01T07:00:00.000Z",
      "2026-11-02T08:00:00.000Z",
    ],
    [
      "2027-01-01T12:00:00Z",
      "2026-12-31T08:00:00.000Z",
      "2027-01-01T08:00:00.000Z",
    ],
  ])("handles clock and year changes on %s", (now, from, before) => {
    vi.stubEnv("TZ", "America/Vancouver");
    expect(itemViewWindow("yesterday", new Date(now))).toEqual({
      from,
      before,
    });
  });
});
