import type { FeedCandidate } from "../types";

export type DiscoveryState =
  | "idle"
  | "loading"
  | "single"
  | "multiple"
  | "none"
  | "error";

export function discoveredCandidateState(
  candidates: FeedCandidate[],
  failed = false,
): Extract<DiscoveryState, "single" | "multiple" | "none" | "error"> {
  if (failed) return "error";
  if (candidates.length === 0) return "none";
  return candidates.length === 1 ? "single" : "multiple";
}
