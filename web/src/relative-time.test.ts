import { describe, expect, it } from "vitest";
import { relativeAgo } from "./relative-time";

describe("relativeAgo", () => {
  const now = Date.parse("2026-10-31T12:00:00Z");

  it("says just now for anything under a minute, never 'now ago'", () => {
    expect(relativeAgo("2026-10-31T11:59:01Z", now)).toBe("just now");
    expect(relativeAgo("2026-10-31T12:00:00Z", now)).toBe("just now");
  });

  it("treats a timestamp slightly in the future as just now", () => {
    expect(relativeAgo("2026-10-31T12:00:30Z", now)).toBe("just now");
  });

  it("puts ago after minutes, hours and days", () => {
    expect(relativeAgo("2026-10-31T11:57:00Z", now)).toBe("3m ago");
    expect(relativeAgo("2026-10-31T10:00:00Z", now)).toBe("2h ago");
    expect(relativeAgo("2026-10-28T12:00:00Z", now)).toBe("3d ago");
  });
});
