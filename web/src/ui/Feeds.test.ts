import { describe, expect, it } from "vitest";
import { archiveSize } from "../archive";
import { formatPrior, whyText } from "../ranking-display";
import type { Item } from "../types";

describe("archive storage estimate", () => {
  it("uses the specified 300 KB per kept item estimate", () => {
    expect(archiveSize(1)).toBe("0.3 MB");
    expect(archiveSize(25)).toBe("7.5 MB");
    expect(archiveSize(124)).toBe("37 MB");
  });
});

describe("ranking diagnostics", () => {
  it("formats signed priors with a typographic minus", () => {
    expect(formatPrior(0.091)).toBe("+0.09");
    expect(formatPrior(0)).toBe("0.00");
    expect(formatPrior(-0.041)).toBe("−0.04");
  });

  it("uses only the two approved why phrasings", () => {
    const item = { why: { title: "A story" } } as Item;
    expect(whyText(item)).toBe("Because you liked: A story");
    item.why = { feed_title: "Example" };
    expect(whyText(item)).toBe("You often like Example");
    item.why = undefined;
    expect(whyText(item)).toBe("");
  });
});
