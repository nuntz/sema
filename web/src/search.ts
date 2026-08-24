import type { Item, SearchResponse } from "./types";

export const SEARCH_DEBOUNCE_MS = 300;

export type SearchSection = {
  key: string;
  label: string;
  suffix: string;
  archive: boolean;
  items: Item[];
};

export function normalizeSearchResponse(
  response?: SearchResponse | null,
): SearchResponse {
  return {
    matches: {
      window: response?.matches?.window ?? [],
      archive: response?.matches?.archive ?? [],
    },
    related: {
      window: response?.related?.window ?? [],
      archive: response?.related?.archive ?? [],
    },
    semantic_available: response?.semantic_available ?? false,
  };
}

export function visibleSearchSections(
  response?: SearchResponse,
): SearchSection[] {
  if (!response) return [];
  const normalized = normalizeSearchResponse(response);
  return [
    {
      key: "matches-window",
      label: "Matches",
      suffix: "This week",
      archive: false,
      items: normalized.matches.window,
    },
    {
      key: "matches-archive",
      label: "Matches",
      suffix: "Archive",
      archive: true,
      items: normalized.matches.archive,
    },
    {
      key: "related-window",
      label: "Related",
      suffix: "This week",
      archive: false,
      items: normalized.related.window,
    },
    {
      key: "related-archive",
      label: "Related",
      suffix: "Archive",
      archive: true,
      items: normalized.related.archive,
    },
  ].filter((section) => section.items.length > 0);
}
