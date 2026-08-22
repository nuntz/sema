import { describe, expect, it } from "vitest";
import { hasLeadingImage } from "./reader-content";

describe("hasLeadingImage", () => {
  it("recognizes direct and wrapped leading images", () => {
    expect(hasLeadingImage(`<img src="lead.jpg"><p>Body</p>`)).toBe(true);
    expect(
      hasLeadingImage(
        `<figure><a href="/full"><img src="lead.jpg"></a><figcaption>Caption</figcaption></figure>`,
      ),
    ).toBe(true);
  });

  it("does not treat later images as the article lead", () => {
    expect(hasLeadingImage(`<p>Introduction</p><img src="later.jpg">`)).toBe(
      false,
    );
    expect(hasLeadingImage("")).toBe(false);
  });
});
