import type { Item } from "./types";

export type FeedbackAction = "signal" | "keep" | "unkeep";
export type FeedbackValue = -1 | 0 | 1;
export interface FeedbackState {
  kept: boolean;
  value: FeedbackValue;
  source?: string;
}

export function applyFeedback(
  state: FeedbackState,
  action: FeedbackAction,
  value: FeedbackValue = 0,
) {
  if (action === "unkeep")
    return { kept: false, value: 0 as const, source: "", preserve: false };
  if (action === "keep")
    return state.value > 0
      ? { ...state, kept: true, source: state.source ?? "", preserve: true }
      : { kept: true, value: 1 as const, source: "heart", preserve: false };
  if (state.kept && value === -1) return undefined;
  return {
    kept: state.kept,
    value: state.kept && value === 0 ? (1 as const) : value,
    source: state.kept && value === 0 ? "heart" : "",
    preserve: false,
  };
}

export function feedbackEligibility(
  item: Pick<Item, "hearted" | "archived">,
  archive = false,
) {
  const live = !archive && !item.archived;
  return {
    signal: live,
    bury: live && !item.hearted,
    lifetime: live && !item.hearted,
  };
}

export function feedbackPatch(
  item: Item,
  action: FeedbackAction,
  value: FeedbackValue = 0,
  archive = false,
) {
  if (action === "signal" && !feedbackEligibility(item, archive).signal)
    return undefined;
  const plan = applyFeedback(
    { kept: item.hearted, value: item.signal },
    action,
    value,
  );
  return plan && { hearted: plan.kept, signal: plan.value };
}
