import { describe, expect, it } from "vitest";
import {
  expiryLabel,
  expiryRingFraction,
  expirySentence,
  expiryState,
  HOUR,
  hoursLeft,
  itemExpiryState,
} from "./expiry";

const now = Date.parse("2026-09-08T12:00:00Z");
const published = (hours: number) =>
  new Date(now - (168 - hours) * HOUR).toISOString();

describe("time left", () => {
  it("uses published time and exact thresholds", () => {
    for (const [hours, state] of [
      [49, "none"],
      [48, "soon"],
      [24, "soon"],
      [23.99, "today"],
      [6, "today"],
      [5.99, "imminent"],
      [0, "imminent"],
    ] as const) {
      expect(hoursLeft(published(hours), now)).toBeCloseTo(hours);
      expect(expiryState(hours)).toBe(state);
    }
    expect(expiryState(Number.NaN)).toBe("none");
  });
  it("kept and archived items never get a marker", () => {
    for (const flags of [{ hearted: true }, { archived: true }]) {
      expect(
        itemExpiryState(
          { hearted: false, published_ts: published(3), ...flags },
          now,
        ),
      ).toBe("none");
    }
  });
  it("shortens small cells without losing the urgent wording", () => {
    for (const [hours, full, compact] of [
      [48, "2d left", "2d"],
      [19, "19h left", "19h"],
      [3, "3h left", "3h left"],
    ] as const) {
      expect(expiryLabel(hours)).toBe(full);
      expect(expiryLabel(hours, { compact: true })).toBe(compact);
    }
  });
  it("empties the ring, retaining a visible minimum", () => {
    expect(expiryRingFraction(48)).toBe(1);
    expect(expiryRingFraction(24)).toBe(0.5);
    expect(expiryRingFraction(-1)).toBe(0.05);
    expect(expiryRingFraction(168)).toBe(1);
  });
  it("provides spoken wording", () => {
    expect(expirySentence(published(48), now)).toBe("2 days left");
    expect(expirySentence(published(19), now)).toBe("19 hours left");
    expect(expirySentence(published(3), now)).toBe("about three hours left");
  });
});
