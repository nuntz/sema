import { createMemo, createSignal } from "solid-js";
import type { AppAPI } from "./api/client";
import { ITEM_WINDOWS, type ItemWindow, windowRange } from "./item-view";
import type { ReadState } from "./read-state";
import { scopeCellModel, windowScopeCounts } from "./scope-cell";
import type { Feed, FeedItemCounts, GridScope } from "./types";

export interface UnreadCountView {
  scope(): GridScope;
  itemWindow(): ItemWindow;
  unreadOnly(): boolean;
  feeds(): Feed[];
  /** The Feeds screen counts All, whatever the Grid's Window. */
  feedsScreen(): boolean;
  phone(): boolean;
}

/**
 * Owns unread-count loading, caching and freshness for every count display.
 * The active Scope and current Window choice subtract Read's optimistic
 * adjustment; Feed, Tag and alternate-Window counts are server snapshots.
 */
export function createUnreadCounts(
  api: Pick<AppAPI, "feedItemCounts">,
  reads: Pick<ReadState, "readAdjust" | "settledSnapshot">,
  view: UnreadCountView,
  handleError: (error: unknown) => void,
) {
  const [counts, setCounts] = createSignal<FeedItemCounts>({});
  // The Window key the counts on hand describe; undefined while unknown.
  const [countsKey, setCountsKey] = createSignal<string>();
  const [windowCounts, setWindowCounts] = createSignal<
    Partial<Record<ItemWindow, FeedItemCounts>>
  >({});
  let generation = 0;
  const pendingWindows = new Map<ItemWindow, number>();

  const windowKey = (window: ItemWindow) => windowRange(window)?.from ?? "";
  // Counts on hand describe the current Window, not one being swapped in.
  const ready = () => countsKey() === windowKey(view.itemWindow());

  const refresh = async () => {
    const current = ++generation;
    setWindowCounts({});
    const feedsScreen = view.feedsScreen();
    const itemWindow = feedsScreen ? "all" : view.itemWindow();
    const window = windowRange(itemWindow);
    const key = windowKey(itemWindow);
    if (countsKey() !== key) setCountsKey(undefined);
    try {
      const snapshot = await reads.settledSnapshot(
        () => current === generation,
        () => api.feedItemCounts(window),
      );
      if (!snapshot?.value || current !== generation) return;
      snapshot.accept((latest) => {
        setCounts(latest);
        setWindowCounts((cached) => ({ ...cached, [itemWindow]: latest }));
        setCountsKey(key);
      });
    } catch (caught) {
      handleError(caught);
    }
  };

  const scopeCell = createMemo(() =>
    scopeCellModel(
      view.scope(),
      view.itemWindow(),
      view.unreadOnly(),
      view.feeds(),
      ready() ? counts() : undefined,
      reads.readAdjust(),
      view.phone(),
    ),
  );

  return {
    refresh,
    /** Refreshes only when the counts on hand are for another Window. */
    refreshIfStale() {
      if (!ready()) void refresh();
    },
    /** True once the counts on hand describe the current Window. */
    ready,
    /** The active Scope's header model, optimistic for Read and Undo. */
    scopeCell,
    /** Per-Feed server counts for the Tag strip and Scope picker. */
    feedCounts: counts,
    /** Per-Feed All counts for the Feeds screen, once known. */
    feedsScreenCounts: () =>
      countsKey() === windowKey("all") ? counts() : undefined,
    /**
     * Counts for each Window choice; the current Window shows
     * `activeCount`, the caller's optimistic active Scope count.
     */
    windowChoices(activeCount: number | undefined) {
      return {
        ...windowScopeCounts(
          view.scope(),
          view.unreadOnly(),
          view.feeds(),
          windowCounts(),
        ),
        [view.itemWindow()]: activeCount,
      };
    },
    /** Loads server counts for Window choices not yet cached. */
    loadWindowChoices() {
      const cached = windowCounts();
      const current = generation;
      for (const { value } of ITEM_WINDOWS) {
        if (
          value === view.itemWindow() ||
          cached[value] ||
          pendingWindows.get(value) === current
        )
          continue;
        pendingWindows.set(value, current);
        void api
          .feedItemCounts(windowRange(value))
          .then((latest) => {
            if (current === generation && latest)
              setWindowCounts((loaded) => ({ ...loaded, [value]: latest }));
          })
          .catch(handleError)
          .finally(() => {
            if (pendingWindows.get(value) === current)
              pendingWindows.delete(value);
          });
      }
    },
  };
}
