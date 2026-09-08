import { describe, expect, it } from "vitest";
import {
  gridSourceName,
  headlineText,
  repeatsLeadHeadline,
} from "./grid-display";

describe("grid display text", () => {
  it("decodes feed punctuation while retaining text as text", () => {
    expect(headlineText("Vancouver&#39;s &amp; &#x2018;Railtown&#x2019;")).toBe(
      "Vancouver's & ‘Railtown’",
    );
    expect(headlineText("&lt;img src=x&gt;")).toBe("<img src=x>");
    expect(headlineText("&#x110000; &#0; &unknown;")).toBe(
      "&#x110000; &#0; &unknown;",
    );
  });
  it("uses concise known feed labels without removing community or custom names", () => {
    expect(
      gridSourceName({ feed_title: "www.theregister.com - Articles" }),
    ).toBe("The Register");
    expect(gridSourceName({ feed_title: "Hacker News: Front Page" })).toBe(
      "Hacker News",
    );
    expect(gridSourceName({ feed_title: "r/vancouver" })).toBe("r/vancouver");
    expect(gridSourceName({ feed_title: "My favourite articles" })).toBe(
      "My favourite articles",
    );
  });
  it("recognises repeated headlines despite typography or a small wording change", () => {
    expect(
      repeatsLeadHeadline(
        "Vancouver’s Railtown & future",
        "Vancouver&#39;s Railtown &amp; future",
      ),
    ).toBe(true);
    expect(
      repeatsLeadHeadline(
        "Smartphone makers don't bother to comply with EU repairability requirements",
        "Smartphone makers don't comply with EU repairability requirements",
      ),
    ).toBe(false);
    expect(
      repeatsLeadHeadline(
        "A European company launches a rocket for the first time",
        "European company launches a rocket for the first time",
      ),
    ).toBe(true);
  });
  it("retains new angles, quantities, and negation instead of hiding them", () => {
    expect(
      repeatsLeadHeadline(
        "Rocket launches for the first time",
        "Industry calls for investment in European launch capacity",
      ),
    ).toBe(false);
    expect(
      repeatsLeadHeadline(
        "Company plans 300 job cuts in its European factories",
        "Company plans 500 job cuts in its European factories",
      ),
    ).toBe(false);
    expect(
      repeatsLeadHeadline(
        "Company plans to close its European factory this year",
        "Company plans not to close its European factory this year",
      ),
    ).toBe(false);
    expect(repeatsLeadHeadline("", "")).toBe(false);
  });
});
