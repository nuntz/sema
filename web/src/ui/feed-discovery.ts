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

const redditEnded =
  "Reddit ended its public feeds, so Sema can’t follow subreddits.";

// The error Add feed shows when discovery fails. Reddit's refusal is keyed on
// its stable kind so the explanation doesn't depend on the server's wording.
export function discoveryErrorMessage(kind: string, message: string): string {
  return kind === "reddit_ended" ? redditEnded : message;
}
