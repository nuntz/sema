import type { Feed, GridScope } from "../types";

export interface TagOption {
  tag: string;
  count: number;
}

export interface FeedScopeOption {
  feedID: string;
  title: string;
  count: number;
  connector: Feed["connector"];
  faviconURL?: string;
}

export type ScopeFilterOption =
  | ({ kind: "tag"; value: string; label: string } & TagOption)
  | ({ kind: "feed"; value: string; label: string } & FeedScopeOption);

export function feedTagOptions(feeds: Feed[]): TagOption[] {
  const counts = new Map<string, number>();
  let untagged = 0;
  for (const feed of feeds) {
    if (feed.muted) continue;
    if (!feed.tags?.length) untagged += feed.item_count ?? 0;
    for (const tag of feed.tags ?? [])
      counts.set(tag, (counts.get(tag) ?? 0) + (feed.item_count ?? 0));
  }
  return [
    ...[...counts]
      .sort(([first], [second]) => first.localeCompare(second))
      .map(([tag, count]) => ({ tag, count })),
    { tag: "untagged", count: untagged },
  ];
}

export function feedScopeOptions(feeds: Feed[]): FeedScopeOption[] {
  return feeds
    .filter((feed) => !feed.muted)
    .map((feed) => ({
      feedID: feed.feed_id,
      title: displayFeedTitle(feed),
      count: feed.item_count ?? 0,
      connector: feed.connector,
      faviconURL: feed.favicon_url,
    }))
    .sort((first, second) => first.title.localeCompare(second.title));
}

export function displayFeedTitle(feed: Feed): string {
  return feed.custom_title || feed.title || domainName(feed.url) || feed.url;
}

export function scopeFilterOptions(
  feeds: Feed[],
  rawQuery = "",
): ScopeFilterOption[] {
  const needle = rawQuery.trim().toLowerCase().replace(/^#/, "");
  const tags: ScopeFilterOption[] = feedTagOptions(feeds)
    .filter((option) => !needle || option.tag.includes(needle))
    .map((option) => ({
      ...option,
      kind: "tag",
      value: option.tag,
      label: option.tag,
    }));
  const feedOptions: ScopeFilterOption[] = feedScopeOptions(feeds)
    .filter((option) => !needle || option.title.toLowerCase().includes(needle))
    .map((option) => ({
      ...option,
      kind: "feed",
      value: option.feedID,
      label: option.title,
    }));
  return [...tags, ...feedOptions];
}

export function scopeOptionID(option: ScopeFilterOption): string {
  return `grid-scope-${option.kind}-${encodeURIComponent(option.value)}`;
}

export function optionScope(
  option: ScopeFilterOption,
): Exclude<GridScope, null> {
  return { kind: option.kind, value: option.value };
}

export function scopeForEnter(
  options: ScopeFilterOption[],
  highlight: number,
): Exclude<GridScope, null> | undefined {
  const option = options[highlight] ?? options[0];
  return option ? optionScope(option) : undefined;
}

export function scopeForClosedEscape(scope: GridScope): null | undefined {
  return scope ? null : undefined;
}

export function feedScopeChip(
  scope: GridScope,
  feeds: Feed[],
): { title: string; ariaLabel: string; option?: FeedScopeOption } | undefined {
  if (scope?.kind !== "feed") return undefined;
  const option = feedScopeOptions(feeds).find(
    (candidate) => candidate.feedID === scope.value,
  );
  const title = option?.title ?? scope.value;
  return { title, ariaLabel: `Clear feed filter: ${title}`, option };
}

function domainName(rawURL: string): string {
  try {
    const parsed = new URL(rawURL);
    return (
      parsed.hostname.replace(/^www\./, "") +
      (parsed.pathname === "/" ? "" : parsed.pathname.replace(/\/$/, ""))
    );
  } catch {
    return rawURL;
  }
}
