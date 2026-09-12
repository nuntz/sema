import { expect, it } from "vitest";
import { shouldRefreshExpiryCounts } from "./expiry-counts";

it("refreshes ordinary polls only after five minutes, and every Expiring poll", () => {
  expect(shouldRefreshExpiryCounts(false, 60_000, 0)).toBe(false);
  expect(shouldRefreshExpiryCounts(false, 120_000, 0)).toBe(false);
  expect(shouldRefreshExpiryCounts(false, 299_999, 0)).toBe(false);
  expect(shouldRefreshExpiryCounts(false, 300_000, 0)).toBe(true);
  expect(shouldRefreshExpiryCounts(true, 60_000, 0)).toBe(true);
});
