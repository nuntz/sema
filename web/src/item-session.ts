import { batch, createSignal } from "solid-js";
import type { AppAPI } from "./api/client";
import { effectiveGridOrder } from "./grid-scope";
import { type FetchWindow, type ItemWindow, windowRange } from "./item-view";
import type { GridClearSnapshot } from "./read-state";
import { normalizeSearchResponse } from "./search";
import type {
  GridScope,
  Item,
  ItemsResponse,
  Order,
  ReadAnchor,
  SearchResponse,
  Story,
} from "./types";

import { frontPageSequence, mergeFrontPage } from "./ui/front-page";

export type ItemSession = ReturnType<typeof createItemSession>;
export function createItemSession(
  api: Pick<AppAPI, "items" | "stories" | "archive">,
  options: {
    onError(error: unknown): void;
    changed(): void;
    beforeReload(): void;
    pagingBlocked(): boolean;
    cleared(): GridClearSnapshot | undefined;
    visible?(): boolean;
  },
) {
  const [items, setItems] = createSignal<Item[]>([]);
  const [stories, setStories] = createSignal<Story[]>([]);
  const [readAnchor, setReadAnchor] = createSignal<ReadAnchor>();
  const [gridIDs, setGridIDs] = createSignal<string[]>([]);
  const [gridStoryIDs, setGridStoryIDs] = createSignal<string[]>([]);
  const [pendingNew, setPendingNew] = createSignal<Item[]>([]);
  const [layoutVersion, setLayoutVersion] = createSignal(0);
  const [scrollTopVersion, setScrollTopVersion] = createSignal(0);
  const [scrollTarget, setScrollTarget] = createSignal(0);
  const [cursor, setCursor] = createSignal("");
  const [hasPage, setHasPage] = createSignal(false);
  const [loading, setLoading] = createSignal(true);
  const [loadingMore, setLoadingMore] = createSignal(false);
  const [readerItem, setReaderItem] = createSignal<Item>();
  const [searchResponse, setSearchResponse] = createSignal<SearchResponse>();
  const [relatedSource, setRelatedSource] = createSignal<Item>();
  const [relatedItems, setRelatedItems] = createSignal<Item[]>([]);
  const [focusedID, setFocusedID] = createSignal("");
  const [order, setOrder] = createSignal<Order>("interest");
  const [scope, setScope] = createSignal<GridScope>(null);
  const [itemWindow, setItemWindow] = createSignal<ItemWindow>("all");
  const [unreadOnly, setUnreadOnly] = createSignal(true);
  const [mode, setMode] = createSignal<"live" | "archive">("live");
  let fetchWindow: FetchWindow | undefined;
  let requestVersion = 0;
  let gridClearVersion = 0;
  let pollTimer: ReturnType<typeof globalThis.setTimeout> | undefined;
  let pollInterval = INITIAL_POLL_INTERVAL;
  let pollGeneration = 0;
  let pollInFlight = false;
  let gridScrollTop = 0;
  let disposed = false;
  const visible =
    options.visible ??
    (() =>
      typeof document === "undefined" ||
      document.visibilityState === "visible");
  const gridOrder = () => effectiveGridOrder(order(), scope());
  const reload = async (
    nextOrder = order(),
    nextUnreadOnly = unreadOnly(),
    nextMode = mode(),
    nextScope = scope(),
  ) => {
    fetchWindow = windowRange(itemWindow());
    options.changed();
    const version = ++requestVersion;
    gridClearVersion++;
    options.beforeReload();
    setLoading(true);
    setHasPage(false);
    setItems([]);
    setStories([]);

    setReadAnchor();
    setGridIDs([]);
    setGridStoryIDs([]);
    setPendingNew([]);
    setCursor("");
    try {
      const includeRead = includeReadForGrid(nextUnreadOnly);
      const requestOrder = effectiveGridOrder(nextOrder, nextScope);
      let page: ItemsResponse;
      let nextStories: Story[] = [];
      if (nextMode === "archive") {
        page = await api.archive("", nextScope);
      } else if (requestOrder === "interest") {
        const [storyPage, itemPage] = await Promise.all([
          api.stories(nextScope, includeRead, fetchWindow),
          api.items(
            requestOrder,
            "",
            includeRead,
            nextScope,
            false,
            fetchWindow,
          ),
        ]);
        nextStories = storyPage.stories ?? [];
        page = {
          ...itemPage,
          items: excludeRenderedStoryItems(itemPage.items ?? [], nextStories),
        };
      } else {
        page = await api.items(
          requestOrder,
          "",
          includeRead,
          nextScope,
          false,
          fetchWindow,
        );
      }
      if (version !== requestVersion) return;
      const pageItems = page.items ?? [];
      const visibleIDs =
        nextMode === "archive"
          ? pageItems.map((item) => item.item_id)
          : visibleItemIDs(pageItems, nextUnreadOnly);
      const nextCursor = page.next_cursor ?? "";
      const nextFocusedID =
        frontPageSequence(
          mergeFrontPage(nextStories, pageItems, Boolean(nextCursor)),
        )[0]?.id ??
        visibleIDs[0] ??
        "";
      batch(() => {
        gridScrollTop = 0;
        setItems(pageItems);
        setStories(nextStories);
        setGridStoryIDs(nextStories.map((story) => story.story_id));
        setReadAnchor(page.read_anchor);
        setGridIDs(visibleIDs);
        setScrollTarget(0);
        setScrollTopVersion((value) => value + 1);
        setCursor(nextCursor);
        setHasPage(true);
        setFocusedID(nextFocusedID);
        setLayoutVersion((value) => value + 1);
      });
    } catch (caught) {
      options.onError(caught);
    } finally {
      if (version === requestVersion) {
        setLoading(false);
      }
    }
  };

  const loadMore = async () => {
    if (options.pagingBlocked()) return;
    if (loadingMore() || !hasPage() || !cursor()) return;
    setLoadingMore(true);
    const version = requestVersion;
    const clearVersion = gridClearVersion;
    const nextCursor = cursor();
    let continueLoading = false;
    try {
      const page =
        mode() === "archive"
          ? await api.archive(nextCursor, scope())
          : await api.items(
              gridOrder(),
              nextCursor,
              includeReadForGrid(unreadOnly()),
              scope(),
              false,
              fetchWindow,
            );
      if (
        version !== requestVersion ||
        clearVersion !== gridClearVersion ||
        nextCursor !== cursor()
      )
        return;
      const pageItems =
        mode() === "live" && gridOrder() === "interest"
          ? excludeRenderedStoryItems(page.items ?? [], stories())
          : (page.items ?? []);
      const seen = new Set(items().map((item) => item.item_id));
      const added = pageItems.filter((item) => !seen.has(item.item_id));
      const visible =
        mode() === "archive"
          ? added.map((item) => item.item_id)
          : visibleItemIDs(added, unreadOnly());
      const responseCursor = page.next_cursor ?? "";
      batch(() => {
        if (added.length > 0) setItems((current) => [...current, ...added]);
        if (visible.length > 0)
          setGridIDs((current) => [...current, ...visible]);
        if (!readAnchor() && page.read_anchor) setReadAnchor(page.read_anchor);
        setCursor(responseCursor);
        setLayoutVersion((value) => value + 1);
      });
      continueLoading = responseCursor !== "" && visible.length === 0;
    } catch (caught) {
      options.onError(caught);
    } finally {
      setLoadingMore(false);
      if (version === requestVersion && continueLoading) void loadMore();
    }
  };

  const pollNew = async (insert = false): Promise<number> => {
    if (
      pollInFlight ||
      mode() === "archive" ||
      loading() ||
      !hasPage() ||
      !visible()
    )
      return 0;
    if (windowRange(itemWindow())?.from !== fetchWindow?.from) {
      await reload();
      return 0;
    }
    const version = requestVersion;
    const clearVersion = gridClearVersion;
    const generation = pollGeneration;
    pollInFlight = true;
    try {
      if (gridOrder() === "interest") {
        const includeRead = includeReadForGrid(unreadOnly());
        const [storyPage, page] = await Promise.all([
          api.stories(scope(), includeRead, fetchWindow, true),
          api.items(
            "interest",
            "",
            includeRead,
            scope(),
            false,
            fetchWindow,
            insert ? 100 : 20,
          ),
        ]);
        if (version !== requestVersion) return 0;
        const incomingStories = storyPage.stories ?? [];
        const pageItems = excludeRenderedStoryItems(
          page.items ?? [],
          incomingStories,
        );
        const incomingItems = [
          ...incomingStories.flatMap((story) => story.items),
          ...pageItems,
        ];
        const currentItems = [
          ...stories().flatMap((story) => story.items),
          ...items(),
        ];
        const unseen = pollCandidates(
          currentItems,
          pendingNew(),
          incomingItems,
          unreadOnly(),
        );
        if (insert && clearVersion === gridClearVersion) {
          const clearedStories = new Set(options.cleared()?.storyIDs);
          const visibleStories = incomingStories.filter(
            (story) => !clearedStories.has(story.story_id),
          );
          const visible = visibleItemIDs(
            pageItems,
            unreadOnly(),
            options.cleared()?.ids,
          );
          const visibleSet = new Set(visible);
          const visibleItems = pageItems.filter((item) =>
            visibleSet.has(item.item_id),
          );
          const nextCursor = page.next_cursor ?? "";
          const nextFocusedID =
            frontPageSequence(
              mergeFrontPage(visibleStories, visibleItems, Boolean(nextCursor)),
            )[0]?.id ??
            visible[0] ??
            "";
          batch(() => {
            gridScrollTop = 0;
            setPendingNew([]);
            if (incomingStories !== stories()) setStories(incomingStories);
            setGridStoryIDs(visibleStories.map((story) => story.story_id));
            setItems(pageItems);
            setReadAnchor(page.read_anchor);
            setGridIDs(visible);
            setScrollTarget(0);
            setScrollTopVersion((value) => value + 1);
            setCursor(nextCursor);
            setFocusedID(nextFocusedID);
            setLayoutVersion((value) => value + 1);
          });
          options.changed();
        } else if (unseen.length > 0) {
          setPendingNew((current) => mergeNewItems(current, unseen));
        }
        if (generation === pollGeneration)
          pollInterval = nextPollInterval(pollInterval, unseen.length > 0);
        return unseen.length;
      }
      const page = await api.items(
        "chrono",
        "",
        includeReadForGrid(unreadOnly()),
        scope(),
        false,
        fetchWindow,
        insert ? 100 : 20,
      );
      if (version !== requestVersion) return 0;
      const unseen = pollCandidates(
        items(),
        pendingNew(),
        page.items ?? [],
        unreadOnly(),
      );
      if (generation === pollGeneration)
        pollInterval = nextPollInterval(pollInterval, unseen.length > 0);
      if (insert && clearVersion === gridClearVersion) {
        const incoming = [...pendingNew(), ...unseen].sort((left, right) =>
          right.fetched_ts.localeCompare(left.fetched_ts),
        );
        return insertNewItems(incoming);
      }
      if (unseen.length > 0) {
        setPendingNew((current) => mergeNewItems(current, unseen));
      }
      return unseen.length;
    } catch (caught) {
      options.onError(caught);
      return 0;
    } finally {
      pollInFlight = false;
      if (insert) schedulePoll();
    }
  };

  const schedulePoll = () => {
    globalThis.clearTimeout(pollTimer);
    if (disposed) return;
    pollTimer = globalThis.setTimeout(async () => {
      await pollNew();
      schedulePoll();
    }, pollInterval);
  };
  const resetPoll = () => {
    pollGeneration++;
    pollInterval = INITIAL_POLL_INTERVAL;
    schedulePoll();
  };
  const insertNewItems = (incoming: Item[]): number => {
    if (incoming.length === 0) return 0;
    const known = new Set(items().map((item) => item.item_id));
    const added = incoming.filter((item) => !known.has(item.item_id));
    setPendingNew([]);
    if (added.length === 0) return 0;
    setItems((current) => mergeNewItems(current, added));
    const visible = visibleItemIDs(added, unreadOnly());
    if (visible.length > 0) {
      setGridIDs((current) => prependGridIDs(current, visible));
      setFocusedID(visible[0]);
      setLayoutVersion((value) => value + 1);
    }
    setScrollTarget(0);
    setScrollTopVersion((value) => value + 1);
    options.changed();
    return added.length;
  };

  const insertPendingNew = () =>
    gridOrder() === "interest" ? pollNew(true) : insertNewItems(pendingNew());

  const replaceItem = (itemID: string, patch: Partial<Item>) => {
    setItems((current) =>
      current.map((item) =>
        item.item_id === itemID ? { ...item, ...patch } : item,
      ),
    );
    setStories((current) => updateStoryItem(current, itemID, patch));
    setSearchResponse((current) =>
      current ? mapSearchItems(current, itemID, patch) : current,
    );
    setRelatedItems((current) =>
      current.map((item) =>
        item.item_id === itemID ? { ...item, ...patch } : item,
      ),
    );
    setRelatedSource((current) =>
      current?.item_id === itemID ? { ...current, ...patch } : current,
    );
    setReaderItem((current) =>
      current?.item_id === itemID ? { ...current, ...patch } : current,
    );
  };

  return {
    items,
    setItems,
    stories,
    setStories,
    readAnchor,
    gridIDs,
    setGridIDs,
    gridStoryIDs,
    setGridStoryIDs,
    pendingNew,
    layoutVersion,
    setLayoutVersion,
    scrollTopVersion,
    setScrollTopVersion,
    scrollTarget,
    setScrollTarget,
    cursor,
    hasPage,
    loading,
    setLoading,
    loadingMore,
    readerItem,
    setReaderItem,
    searchResponse,
    setSearchResponse,
    relatedSource,
    setRelatedSource,
    relatedItems,
    setRelatedItems,
    focusedID,
    setFocusedID,
    order,
    setOrder,
    scope,
    setScope,
    itemWindow,
    setItemWindow,
    unreadOnly,
    setUnreadOnly,
    mode,
    setMode,
    reload,
    loadMore,
    pollNew,
    resetPoll,
    insertPendingNew,
    replaceItem,
    get fetchWindow() {
      return fetchWindow;
    },
    get version() {
      return requestVersion;
    },
    get clearVersion() {
      return gridClearVersion;
    },
    invalidateGrid() {
      gridClearVersion++;
    },
    get scrollTop() {
      return gridScrollTop;
    },
    set scrollTop(top: number) {
      gridScrollTop = top;
    },
    dispose() {
      disposed = true;
      requestVersion++;
      globalThis.clearTimeout(pollTimer);
    },
  };
}
function mapSearchItems(
  response: SearchResponse,
  itemID: string,
  patch: Partial<Item>,
): SearchResponse {
  response = normalizeSearchResponse(response);
  const map = (items: Item[]) =>
    items.map((item) =>
      item.item_id === itemID ? { ...item, ...patch } : item,
    );
  return {
    ...response,
    matches: {
      window: map(response.matches.window),
      archive: map(response.matches.archive),
    },
    related: {
      window: map(response.related.window),
      archive: map(response.related.archive),
    },
  };
}

export function prependGridIDs(
  current: string[],
  incoming: string[],
): string[] {
  if (incoming.length === 0) return current;
  const added = new Set(incoming);
  return [...incoming, ...current.filter((id) => !added.has(id))];
}

export function includeReadForGrid(unreadOnly: boolean): boolean {
  return !unreadOnly;
}

export function mergeNewItems(current: Item[], incoming: Item[]): Item[] {
  const seen = new Set(current.map((item) => item.item_id));
  const added = incoming.filter((item) => {
    if (seen.has(item.item_id)) return false;
    seen.add(item.item_id);
    return true;
  });
  return added.length > 0 ? [...added, ...current] : current;
}

export function pollCandidates(
  loaded: Item[],
  pending: Item[],
  incoming: Item[],
  unreadOnly: boolean,
): Item[] {
  const known = new Set([...loaded, ...pending].map((item) => item.item_id));
  const newestFetch = loaded.reduce(
    (newest, item) => (item.fetched_ts > newest ? item.fetched_ts : newest),
    "",
  );
  return incoming.filter(
    (item) =>
      !known.has(item.item_id) &&
      item.fetched_ts > newestFetch &&
      (!unreadOnly || !item.read),
  );
}

export function visibleItemIDs(
  items: Item[],
  unreadOnly: boolean,
  excludedIDs: Iterable<string> = [],
): string[] {
  const excluded = new Set(excludedIDs);
  return items
    .filter(
      (item) => !excluded.has(item.item_id) && (!unreadOnly || !item.read),
    )
    .map((item) => item.item_id);
}

export const INITIAL_POLL_INTERVAL = 3 * 60_000;

export function nextPollInterval(current: number, foundItems: boolean): number {
  return foundItems
    ? INITIAL_POLL_INTERVAL
    : Math.min(current * 2, 15 * 60_000);
}

export function excludeRenderedStoryItems(
  items: Item[],
  stories: Story[],
): Item[] {
  if (items.length === 0 || stories.length === 0) return items;
  const hidden = new Set(
    stories.flatMap((story) => story.items.map((item) => item.item_id)),
  );
  return items.filter((item) => !hidden.has(item.item_id));
}

export function updateStoryItem(
  stories: Story[],
  itemID: string,
  patch: Partial<Item>,
): Story[] {
  return stories.map((story) => ({
    ...story,
    items: story.items.map((item) =>
      item.item_id === itemID ? { ...item, ...patch } : item,
    ),
  }));
}
