import { describe, expect, it } from "vitest";
import cases from "../../testdata/feedback.json";
import {
  applyFeedback,
  type FeedbackAction,
  type FeedbackValue,
  feedbackEligibility,
} from "./feedback";

describe("Feedback", () => {
  for (const rule of cases) {
    it(rule.name, () => {
      const plan = applyFeedback(
        { kept: rule.kept, value: rule.current as FeedbackValue },
        rule.action as FeedbackAction,
        (rule.requested ?? 0) as FeedbackValue,
      );
      if (rule.rejected) expect(plan).toBeUndefined();
      else
        expect(plan).toEqual({
          kept: rule.nextKept,
          value: rule.value,
          source: rule.source,
          preserve: rule.preserve ?? false,
        });
    });
  }
  it("archive copies have no live feedback or lifetime", () => {
    expect(feedbackEligibility({ hearted: true, archived: true })).toEqual({
      signal: false,
      bury: false,
      lifetime: false,
    });
  });
});
