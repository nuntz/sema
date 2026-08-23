import { describe, expect, it } from "vitest";
import { archiveSize } from "../archive";

describe("archive storage estimate", () => {
  it("uses the specified 300 KB per kept item estimate", () => {
    expect(archiveSize(1)).toBe("0.3 MB");
    expect(archiveSize(25)).toBe("7.5 MB");
    expect(archiveSize(124)).toBe("37 MB");
  });
});
