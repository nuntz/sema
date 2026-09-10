import { ITEM_VIEWS, type ItemView } from "./item-view";
import type { Feed, FeedItemCounts, GridScope } from "./types";
import { displayFeedTitle } from "./ui/tag-options";

export interface ScopeCellModel {
  title: string;
  faviconFeed?: Feed;
  count?: number;
  feeds?: number;
  qualifier: string;
  text: string;
}

export function formatScopeMeta(
  count: number | undefined,
  qualifier: string,
  feeds?: number,
  phone = false,
): string {
  const amount =
    count === undefined ? "counting" : count.toLocaleString("en-US");
  const unit = count === 1 ? "item" : "items";
  const words =
    qualifier === "unread"
      ? phone
        ? "unread"
        : `unread ${unit}`
      : `${unit}${qualifier ? ` ${qualifier}` : ""}`;
  return `${amount} ${words}${!phone && feeds !== undefined ? ` · ${feeds.toLocaleString("en-US")} ${feeds === 1 ? "feed" : "feeds"}` : ""}`;
}

export function scopeCellModel(
  scope: GridScope,
  itemView: ItemView,
  feeds: Feed[],
  counts: FeedItemCounts | undefined,
  readAdjust: number,
  phone: boolean,
): ScopeCellModel {
  const selected = feeds.filter(
    (feed) =>
      !feed.muted &&
      (!scope ||
        (scope.kind === "feed"
          ? feed.feed_id === scope.value
          : scope.value === "untagged"
            ? !feed.tags?.length
            : feed.tags?.includes(scope.value))),
  );
  const faviconFeed =
    scope?.kind === "feed"
      ? feeds.find((feed) => feed.feed_id === scope.value)
      : undefined;
  const title =
    scope?.kind === "tag"
      ? `#${scope.value}`
      : scope?.kind === "feed"
        ? faviconFeed
          ? displayFeedTitle(faviconFeed)
          : scope.value
        : (ITEM_VIEWS.find((view) => view.value === itemView)?.label ??
          itemView);
  const contributions =
    counts &&
    selected.map(
      (feed) =>
        counts[feed.feed_id]?.[itemView === "unread" ? "unread" : "all"] ?? 0,
    );
  const count = contributions
    ? Math.max(
        0,
        contributions.reduce((sum, value) => sum + value, 0) -
          (itemView === "unread" ? readAdjust : 0),
      )
    : undefined;
  const contributingFeeds =
    scope?.kind === "feed" || (!scope && count === 0)
      ? undefined
      : contributions?.filter((value) => value > 0).length;
  const qualifier =
    itemView === "unread"
      ? "unread"
      : (itemView === "today" || itemView === "yesterday") &&
          (scope || phone || count === 0)
        ? itemView
        : "";
  return {
    title,
    faviconFeed,
    count,
    feeds: contributingFeeds,
    qualifier,
    text: formatScopeMeta(count, qualifier, contributingFeeds, phone),
  };
}
