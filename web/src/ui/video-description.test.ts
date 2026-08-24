import { describe, expect, it } from "vitest";
import { parseVideoDescription, timestampURL } from "./video-description";

describe("video descriptions", () => {
  it("linkifies short and hour timestamps at the correct video offset", () => {
    const blocks = parseVideoDescription(
      "Start at 1:23 and revisit 01:02:03.",
      "https://www.youtube.com/watch?v=abc",
    );
    const tokens = blocks[0]?.kind === "paragraph" ? blocks[0].tokens : [];
    expect(tokens.filter((token) => token.kind === "timestamp")).toEqual([
      expect.objectContaining({ text: "1:23", seconds: 83 }),
      expect.objectContaining({ text: "01:02:03", seconds: 3723 }),
    ]);
    expect(timestampURL("https://www.youtube.com/watch?v=abc", 83)).toContain(
      "&t=83s",
    );
  });

  it("groups three line-initial timestamps as chapters", () => {
    const blocks = parseVideoDescription(
      "0:00 Intro\n1:23 Details\n01:02:03 Finale",
      "https://www.youtube.com/watch?v=abc",
    );
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.kind).toBe("chapters");
    if (blocks[0]?.kind === "chapters") expect(blocks[0].rows).toHaveLength(3);
  });

  it("drops handles, tags, calls to subscribe, affiliate lines, and emoji runs", () => {
    const blocks = parseVideoDescription(
      "@channel\n#tag\nSUBSCRIBE now\nAffiliate links below\n😀 😀 😀 😀\nUseful https://example.com/path/",
      "https://www.youtube.com/watch?v=abc",
    );
    expect(blocks).toHaveLength(1);
    expect(JSON.stringify(blocks)).toContain("example.com/path");
    expect(JSON.stringify(blocks)).not.toContain("SUBSCRIBE");
  });
});
