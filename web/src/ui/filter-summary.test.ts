import { describe, expect, it } from "vitest";
import { filterSummaryDescription, filterSummaryLabel } from "./filter-summary";

describe("filterSummaryLabel", () => {
  it("shows only what differs from the defaults", () => {
    expect(filterSummaryLabel("interest", "all", false)).toBe("All");
    expect(filterSummaryLabel("interest", "all", true)).toBe("Unread");
    expect(filterSummaryLabel("chrono", "all", true)).toBe("Latest · Unread");
    expect(filterSummaryLabel("interest", "today", false)).toBe("Today");
    expect(filterSummaryLabel("chrono", "yesterday", true)).toBe(
      "Latest · Yesterday · Unread",
    );
  });
});

describe("filterSummaryDescription", () => {
  it("spells out every setting and the count as a sentence", () => {
    expect(filterSummaryDescription("interest", "all", true, 183)).toBe(
      "Filter: Front page, All dates, Unread only, 183 items",
    );
    expect(filterSummaryDescription("chrono", "today", false, 1)).toBe(
      "Filter: Latest, Today, All items, 1 item",
    );
  });
  it("says counting while the count is unknown", () => {
    expect(filterSummaryDescription("interest", "all", true, undefined)).toBe(
      "Filter: Front page, All dates, Unread only, counting",
    );
  });
});
