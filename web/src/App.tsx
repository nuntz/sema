// biome-ignore-all lint/a11y/useSemanticElements: The settled header contract requires button elements with radio roles.

import {
  batch,
  createEffect,
  createMemo,
  createSignal,
  For,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import { APIClient, UnauthorizedError } from "./api/client";
import { completeArchiveRemoval, shouldConfirmArchiveRemoval } from "./archive";
import {
  type BehaviourEvent,
  linkBehaviourEvent,
  mergeBehaviourEvent,
} from "./behaviour-events";
import { AppHeader } from "./components/AppHeader";
import { Icon } from "./components/Icon";
import { ThemeToggle } from "./components/ThemeToggle";
import { Tooltip } from "./components/Tooltip";
import { effectiveGridOrder, sameGridScope } from "./grid-scope";
import {
  finishAndClearGrid,
  type GridClearSnapshot,
  includeReadForGrid,
  mergeNewItems,
  pollCandidates,
  prependGridIDs,
  unreadIDsAfter,
  updateRead,
  visibleItemIDs,
} from "./item-list";
import {
  type FetchWindow,
  ITEM_VIEWS,
  type ItemView,
  itemViewWindow,
} from "./item-view";
import {
  copyOriginalLink,
  isCancelledShare,
  LinkActionFailure,
} from "./link-action";
import { createMediaQuery } from "./media-query";
import { PendingReads } from "./pending-reads";
import { resolveReaderItem } from "./reader-item";
import { scopeCellModel } from "./scope-cell";
import { normalizeSearchResponse, SEARCH_DEBOUNCE_MS } from "./search";
import {
  excludeRenderedStoryItems,
  updateStoriesRead,
  updateStoryItem,
} from "./story-state";
import { nextThemePreference, type ThemeController } from "./theme";
import type {
  Feed,
  FeedItemCounts,
  GridScope,
  Item,
  ItemsResponse,
  Order,
  Profile,
  ReadAnchor,
  SearchResponse,
  Story,
} from "./types";
import { ConfirmRemove } from "./ui/ConfirmRemove";
import { Feeds } from "./ui/Feeds";
import {
  frontPageEntriesForState,
  frontPageSequence,
  frontPageUnreadIDsAfter,
  hasWithheldStories,
  mergeFrontPage,
} from "./ui/front-page";
import { Grid } from "./ui/Grid";
import { KeyboardMap } from "./ui/KeyboardMap";
import {
  appCommand,
  characterShortcut,
  type GoCommand,
  goCommand,
  isEditingTarget,
  readCharacterShortcuts,
  scopeShortcuts,
} from "./ui/keyboard";
import { closeOverlay, pushOverlay } from "./ui/overlay-history";
import { Reader } from "./ui/Reader";
import { RelatedPanel } from "./ui/RelatedPanel";
import { SearchResults } from "./ui/SearchResults";
import { TagFilter } from "./ui/TagFilter";
import { displayFeedTitle } from "./ui/tag-options";
import { listenForWindowReturn } from "./window-activity";

type Undo = {
  ids: string[];
  gridSnapshot?: GridClearSnapshot & { count: number };
};
type Toast = {
  id: number;
  kind: "success" | "info" | "error";
  message: string;
};
type HeaderMenu = "combined" | "overflow";
const READER_EXIT_MS = 220;

export function App(props: { signOut(): void; theme: ThemeController }) {
  const api = new APIClient();
  const [, setProfile] = createSignal<Profile>();
  const [heartCount, setHeartCount] = createSignal(0);
  const [order, setOrder] = createSignal<Order>("interest");
  const [scope, setScope] = createSignal<GridScope>(null);
  const [feedFilters, setFeedFilters] = createSignal<Feed[]>([]);
  const [feedItemCounts, setFeedItemCounts] = createSignal<FeedItemCounts>({});
  const [scopeCountsWindowKey, setScopeCountsWindowKey] =
    createSignal<string>();
  const [readAdjust, setReadAdjust] = createSignal(0);
  const scopePhone = createMediaQuery("(max-width: 619px)");

  const [items, setItems] = createSignal<Item[]>([]);
  const [stories, setStories] = createSignal<Story[]>([]);
  const [expandedStoryIDs, setExpandedStoryIDs] = createSignal<Set<string>>(
    new Set(),
  );
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
  const [error, setError] = createSignal("");
  const [itemView, setItemView] = createSignal<ItemView>("unread");
  const unreadOnly = () => itemView() === "unread";
  let fetchWindow: FetchWindow | undefined;
  const [focusedID, setFocusedID] = createSignal("");
  const [readerID, setReaderID] = createSignal("");
  const [readerItem, setReaderItem] = createSignal<Item>();
  const [readerClosing, setReaderClosing] = createSignal(false);
  const [readerReveal, setReaderReveal] = createSignal(0);
  const [readerDragging, setReaderDragging] = createSignal(false);
  const [mode, setMode] = createSignal<"live" | "archive">("live");
  const [confirmRemove, setConfirmRemove] = createSignal<Item>();
  const [keysOpen, setKeysOpen] = createSignal(false);
  const [characterShortcuts, setCharacterShortcuts] = createSignal(
    readCharacterShortcuts(),
  );
  const [linkActionID, setLinkActionID] = createSignal("");
  const [toast, setToast] = createSignal<Toast>();
  const [view, setView] = createSignal<"grid" | "feeds">("grid");
  const [undo, setUndo] = createSignal<Undo>();
  const [searchQuery, setSearchQuery] = createSignal("");
  const [searchResponse, setSearchResponse] = createSignal<SearchResponse>();
  const [searchLoading, setSearchLoading] = createSignal(false);
  const [searchFocused, setSearchFocused] = createSignal(false);
  const [searchFocusedID, setSearchFocusedID] = createSignal("");
  const [relatedSource, setRelatedSource] = createSignal<Item>();
  const [relatedItems, setRelatedItems] = createSignal<Item[]>([]);
  const [relatedLoading, setRelatedLoading] = createSignal(false);
  const [readerArchive, setReaderArchive] = createSignal(false);
  const [headerMenu, setHeaderMenu] = createSignal<HeaderMenu>();
  const phoneHeader = createMediaQuery("(max-width: 430px)");
  const compactDisplayControls = createMediaQuery("(max-width: 859px)");
  const [tagFilterOpen, setTagFilterOpen] = createSignal(false);
  const [tagOpenRequest, setTagOpenRequest] = createSignal(0);
  let requestVersion = 0;
  let feedItemCountVersion = 0;
  let gridClearVersion = 0;
  let searchVersion = 0;
  let relatedVersion = 0;
  let readTimer: number | undefined;
  const readFlushes = new Set<Promise<void>>();
  let readerCloseTimer: number | undefined;
  let pollTimer: number | undefined;
  let pollInFlight = false;
  let markBelowInFlight = false;
  let goPending = false;
  let goTimer: number | undefined;
  let gridScrollTop = 0;
  let feedsGridDirty = false;
  let feedFilterRefresh: Promise<void> | undefined;
  let linkActionTimer: number | undefined;
  let toastTimer: number | undefined;
  let undoTimer: number | undefined;
  let undoDeadline = 0;
  let undoRemaining = 8_000;
  let undoHovered = false;
  let undoFocused = false;
  let toastID = 0;
  let searchInput!: HTMLInputElement;
  const pendingRead = new PendingReads();
  let disposed = false;
  const pendingEvents = new Map<string, BehaviourEvent>();
  const heartsInFlight = new Set<string>();
  const searchActive = createMemo(() => [...searchQuery().trim()].length >= 2);
  const searchOpen = createMemo(
    () => searchFocused() || searchQuery().length > 0,
  );
  const finishUndo = createMemo(() => undo()?.gridSnapshot);
  const headerTooltipDisabled = createMemo(
    () => searchOpen() || tagFilterOpen() || Boolean(headerMenu()),
  );
  const gridOrder = createMemo(() => effectiveGridOrder(order(), scope()));
  const feedScoped = createMemo(() => scope()?.kind === "feed");
  const orderLabel = createMemo(() =>
    gridOrder() === "interest" ? "Front page" : "Latest",
  );
  const activeFeedTitle = createMemo(() => {
    const current = scope();
    if (current?.kind !== "feed") return "";
    const feed = feedFilters().find(
      (candidate) => candidate.feed_id === current.value,
    );
    return feed ? displayFeedTitle(feed) : current.value;
  });

  const handleError = (caught: unknown) => {
    if (caught instanceof UnauthorizedError) {
      props.signOut();
      return;
    }
    setError(
      caught instanceof Error ? caught.message : "Something went wrong.",
    );
  };

  const showToast = (kind: Toast["kind"], message: string) => {
    window.clearTimeout(toastTimer);
    setToast({ id: ++toastID, kind, message });
    toastTimer = window.setTimeout(() => setToast(), 2_320);
  };

  const clearUndoTimer = () => {
    window.clearTimeout(undoTimer);
    undoTimer = undefined;
    undoDeadline = 0;
  };

  const expireFinishUndo = () => {
    const snapshot = undo()?.gridSnapshot;
    clearUndoTimer();
    undoRemaining = 8_000;
    setUndo((current) =>
      current?.gridSnapshot === snapshot ? undefined : current,
    );
  };

  const resumeFinishUndoTimer = () => {
    if (!finishUndo() || undoTimer || undoHovered || undoFocused) return;
    undoDeadline = Date.now() + undoRemaining;
    undoTimer = window.setTimeout(expireFinishUndo, undoRemaining);
  };

  const pauseFinishUndoTimer = () => {
    if (!undoTimer) return;
    undoRemaining = Math.max(0, undoDeadline - Date.now());
    clearUndoTimer();
  };

  const syncFinishUndoTimer = () => {
    if (undoHovered || undoFocused) pauseFinishUndoTimer();
    else resumeFinishUndoTimer();
  };

  const startFinishUndoTimer = () => {
    clearUndoTimer();
    undoRemaining = 8_000;
    undoHovered = false;
    undoFocused = false;
    resumeFinishUndoTimer();
  };

  const discardFinishUndo = () => {
    clearUndoTimer();
    undoRemaining = 8_000;
    setUndo((current) => (current?.gridSnapshot ? undefined : current));
  };

  const copyLink = async (item: Item) => {
    const recordBehaviour = !item.archived;
    try {
      const action = await copyOriginalLink({
        url: item.url,
        title: item.title,
      });
      window.clearTimeout(linkActionTimer);
      setLinkActionID(item.item_id);
      linkActionTimer = window.setTimeout(() => setLinkActionID(""), 900);
      showToast("success", action === "shared" ? "Link shared" : "Link copied");
      if (recordBehaviour) queueEvent(item.item_id, linkBehaviourEvent());
    } catch (caught) {
      if (isCancelledShare(caught)) return;
      const message =
        caught instanceof LinkActionFailure && caught.action === "shared"
          ? "Couldn't share"
          : "Couldn't copy";
      showToast("error", message);
    }
  };

  const scopeCell = createMemo(() => {
    if (mode() === "archive" || searchActive()) return undefined;
    const model = scopeCellModel(
      scope(),
      itemView(),
      feedFilters(),
      scopeCountsWindowKey() === (itemViewWindow(itemView())?.from ?? "")
        ? feedItemCounts()
        : undefined,
      readAdjust(),
      scopePhone(),
    );
    return !scope() && itemView() === "unread" && model.count === 0
      ? undefined
      : model;
  });

  const bootstrap = async () => {
    setLoading(true);
    setError("");
    try {
      const me = await api.me();
      setProfile(me.profile);
      setHeartCount(me.heart_count ?? me.profile.heart_count ?? 0);
      setOrder(me.profile.order_pref || "interest");
      const profileScope: GridScope = me.profile.feed_pref
        ? { kind: "feed", value: me.profile.feed_pref }
        : me.profile.tag_pref
          ? { kind: "tag", value: me.profile.tag_pref }
          : null;
      setScope(profileScope);
      const [availableFeeds] = await Promise.all([
        api.feeds(),
        reload(
          me.profile.order_pref || "interest",
          unreadOnly(),
          "live",
          profileScope,
        ),
      ]);
      setFeedFilters(availableFeeds);
    } catch (caught) {
      handleError(caught);
    } finally {
      setLoading(false);
    }
  };

  const reload = async (
    nextOrder = order(),
    nextUnreadOnly = unreadOnly(),
    nextMode = mode(),
    nextScope = scope(),
  ) => {
    fetchWindow = itemViewWindow(itemView());
    void refreshFeedItemCounts();
    const version = ++requestVersion;
    gridClearVersion++;
    discardFinishUndo();
    setLoading(true);
    setHasPage(false);
    setItems([]);
    setStories([]);
    setExpandedStoryIDs(new Set<string>());
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
      handleError(caught);
    } finally {
      if (version === requestVersion) setLoading(false);
    }
  };

  const loadMore = async () => {
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
      const loadedItems = added.length > 0 ? [...items(), ...added] : items();
      batch(() => {
        if (added.length > 0) setItems((current) => [...current, ...added]);
        if (visible.length > 0)
          setGridIDs((current) => [...current, ...visible]);
        if (!readAnchor() && page.read_anchor) setReadAnchor(page.read_anchor);
        setCursor(responseCursor);
        setLayoutVersion((value) => value + 1);
      });
      continueLoading =
        responseCursor !== "" &&
        (visible.length === 0 ||
          hasWithheldStories(stories(), loadedItems, true));
    } catch (caught) {
      handleError(caught);
    } finally {
      setLoadingMore(false);
      if (continueLoading) void loadMore();
    }
  };

  const pollNew = async (insert = false): Promise<number> => {
    if (
      pollInFlight ||
      mode() === "archive" ||
      loading() ||
      !hasPage() ||
      document.visibilityState !== "visible"
    )
      return 0;
    if (itemViewWindow(itemView())?.from !== fetchWindow?.from) {
      await reload();
      return 0;
    }
    const version = requestVersion;
    const clearVersion = gridClearVersion;
    pollInFlight = true;
    try {
      if (gridOrder() === "interest") {
        const includeRead = includeReadForGrid(unreadOnly());
        const [storyPage, page] = await Promise.all([
          api.stories(scope(), includeRead, fetchWindow),
          api.items("interest", "", includeRead, scope(), false, fetchWindow),
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
          const clearedStories = new Set(finishUndo()?.storyIDs);
          const visibleStories = incomingStories.filter(
            (story) => !clearedStories.has(story.story_id),
          );
          const visible = visibleItemIDs(
            pageItems,
            unreadOnly(),
            finishUndo()?.ids,
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
            setStories(incomingStories);
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
          void refreshFeedItemCounts();
        } else if (unseen.length > 0) {
          setPendingNew((current) => mergeNewItems(current, unseen));
        }
        return unseen.length;
      }
      const page = await api.items(
        "chrono",
        "",
        includeReadForGrid(unreadOnly()),
        scope(),
        false,
        fetchWindow,
      );
      if (version !== requestVersion) return 0;
      const unseen = pollCandidates(
        items(),
        pendingNew(),
        page.items ?? [],
        unreadOnly(),
      );
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
      handleError(caught);
      return 0;
    } finally {
      pollInFlight = false;
    }
  };

  createEffect(() => {
    const query = searchQuery().trim();
    const version = ++searchVersion;
    if ([...query].length < 2) {
      setSearchResponse();
      setSearchFocusedID("");
      setSearchLoading(false);
      return;
    }
    setSearchLoading(true);
    const timer = window.setTimeout(() => {
      api
        .search(query)
        .then((result) => {
          if (version !== searchVersion) return;
          const normalized = normalizeSearchResponse(result);
          setSearchResponse(normalized);
          const first = [
            ...normalized.matches.window,
            ...normalized.matches.archive,
            ...normalized.related.window,
            ...normalized.related.archive,
          ][0];
          setSearchFocusedID(first?.item_id ?? "");
        })
        .catch((caught) => {
          if (version === searchVersion) handleError(caught);
        })
        .finally(() => {
          if (version === searchVersion) setSearchLoading(false);
        });
    }, SEARCH_DEBOUNCE_MS);
    onCleanup(() => window.clearTimeout(timer));
  });

  const clearSearch = () => {
    searchVersion++;
    setSearchQuery("");
    setSearchResponse();
    setSearchLoading(false);
    setSearchFocusedID("");
    setSearchFocused(false);
    searchInput?.blur();
  };

  let searchInHistory = false;
  createEffect(() => {
    const open = phoneHeader() && searchOpen();
    if (open === searchInHistory) return;
    searchInHistory = open;
    if (open)
      pushOverlay("search", () => {
        searchInHistory = false;
        clearSearch();
      });
    else closeOverlay("search");
  });

  const openKeys = () => {
    if (keysOpen()) return;
    pushOverlay("keyboard-help", () => setKeysOpen(false));
    setKeysOpen(true);
  };

  const closeKeys = () => {
    if (!keysOpen()) return;
    closeOverlay("keyboard-help");
    setKeysOpen(false);
  };

  const focusSearch = () => {
    setHeaderMenu();
    setSearchFocused(true);
    queueMicrotask(() => {
      searchInput?.focus();
      searchInput?.select();
    });
  };

  const clearGo = () => {
    goPending = false;
    window.clearTimeout(goTimer);
    goTimer = undefined;
  };

  const goSequenceAvailable = () =>
    !keysOpen() &&
    !headerMenu() &&
    !tagFilterOpen() &&
    !document.querySelector("[role=dialog]") &&
    (view() === "feeds" ||
      (view() === "grid" &&
        !readerID() &&
        !confirmRemove() &&
        !searchActive() &&
        !relatedSource()));

  onMount(() => {
    bootstrap();
    const flush = () => void flushPending(true);
    const stopWindowReturn = listenForWindowReturn(flush, () => void pollNew());
    const onShortcutCapture = (event: KeyboardEvent) => {
      if (event.isComposing || isEditingTarget(event.target)) {
        clearGo();
        return;
      }
      if (!characterShortcuts() && characterShortcut(event)) {
        clearGo();
        event.stopImmediatePropagation();
        return;
      }
      if (event.repeat || event.ctrlKey || event.metaKey || event.altKey) {
        clearGo();
        return;
      }
      if (goPending) {
        const command = goCommand(event.key);
        clearGo();
        if (command && goSequenceAvailable()) {
          event.preventDefault();
          void navigateByKey(command);
        }
      } else if (event.key === "g" && goSequenceAvailable()) {
        goPending = true;
        goTimer = window.setTimeout(clearGo, 600);
      }
    };
    const clearGoOnFocus = () => clearGo();
    const onKeyDown = (event: KeyboardEvent) => {
      // A reader lightbox owns its key card and suspends global view shortcuts.
      if (document.querySelector(".lb-overlay")) return;
      if (event.defaultPrevented || event.isComposing) return;
      const target = event.target;
      const editing =
        target instanceof HTMLElement &&
        (target.isContentEditable || target.matches("input, textarea, select"));
      if (
        !editing &&
        !event.shiftKey &&
        !event.altKey &&
        (event.metaKey || event.ctrlKey) &&
        event.key.toLowerCase() === "z" &&
        finishUndo() &&
        view() === "grid" &&
        mode() === "live" &&
        unreadOnly() &&
        !readerID() &&
        !keysOpen() &&
        !confirmRemove() &&
        !searchActive() &&
        !relatedSource()
      ) {
        event.preventDefault();
        undoLast();
        return;
      }
      if (event.key === "/" && !readerID() && !relatedSource() && !editing) {
        event.preventDefault();
        focusSearch();
        return;
      }
      if (event.key === "Escape" && headerMenu()) {
        event.preventDefault();
        setHeaderMenu();
        return;
      }
      if (editing) {
        clearGo();
        return;
      }
      if (event.repeat || event.metaKey || event.ctrlKey || event.altKey) {
        clearGo();
        return;
      }
      if (event.key === "Escape" && view() === "feeds" && !keysOpen()) {
        event.preventDefault();
        void closeFeedsAndSettings();
        return;
      }
      const command = appCommand(event.key);
      if (!command || (command === "close-help" && !keysOpen())) return;
      if (
        command === "toggle-unread" &&
        (view() !== "grid" ||
          mode() !== "live" ||
          loading() ||
          !!readerID() ||
          keysOpen() ||
          !!confirmRemove() ||
          searchActive() ||
          !!relatedSource())
      )
        return;
      event.preventDefault();
      if (command === "toggle-archive") {
        closeKeys();
        void toggleArchive();
      } else if (command === "toggle-unread") {
        void toggleUnread();
      } else {
        if (command === "toggle-help" && !keysOpen()) openKeys();
        else closeKeys();
      }
    };
    pollTimer = window.setInterval(() => void pollNew(), 60_000);
    window.addEventListener("keydown", onShortcutCapture, true);
    window.addEventListener("blur", clearGoOnFocus);
    window.addEventListener("focusin", clearGoOnFocus);
    window.addEventListener("pagehide", flush);
    window.addEventListener("keydown", onKeyDown);
    onCleanup(() => {
      disposed = true;
      window.removeEventListener("keydown", onShortcutCapture, true);
      window.removeEventListener("blur", clearGoOnFocus);
      window.removeEventListener("focusin", clearGoOnFocus);
      stopWindowReturn();
      window.removeEventListener("pagehide", flush);
      window.removeEventListener("keydown", onKeyDown);
      window.clearTimeout(readTimer);
      window.clearTimeout(readerCloseTimer);
      window.clearInterval(pollTimer);
      window.clearTimeout(linkActionTimer);
      window.clearTimeout(toastTimer);
      window.clearTimeout(undoTimer);
      window.clearTimeout(goTimer);
      void flushPending(true);
    });
  });

  const gridItems = createMemo(() => {
    const byID = new Map(items().map((item) => [item.item_id, item]));
    return gridIDs().flatMap((id) => {
      const item = byID.get(id);
      return item ? [item] : [];
    });
  });
  const gridStories = createMemo(() => {
    const visible = new Set(gridStoryIDs());
    return stories().filter((story) => visible.has(story.story_id));
  });
  const frontPageEntries = createMemo(() =>
    frontPageEntriesForState(
      gridStories(),
      gridItems(),
      mode(),
      gridOrder(),
      cursor(),
    ),
  );
  const frontPageItems = createMemo(() =>
    frontPageSequence(frontPageEntries(), expandedStoryIDs()).map(
      ({ item }) => item,
    ),
  );
  const searchItems = createMemo(() => {
    const result = searchResponse();
    const normalized = normalizeSearchResponse(result);
    return result
      ? [
          ...normalized.matches.window,
          ...normalized.matches.archive,
          ...normalized.related.window,
          ...normalized.related.archive,
        ]
      : [];
  });
  const selected = createMemo(() => {
    const id = readerID();
    return resolveReaderItem(
      id,
      [
        items(),
        stories().flatMap((story) => story.items),
        searchItems(),
        relatedItems(),
      ],
      readerItem(),
    );
  });
  const selectedIndex = createMemo(() =>
    frontPageItems().findIndex((item) => item.item_id === readerID()),
  );

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

  const openRelated = (item: Item) => {
    if (!relatedSource())
      pushOverlay("related", () => {
        relatedVersion++;
        setRelatedSource();
        setRelatedItems([]);
        setRelatedLoading(false);
      });
    const version = ++relatedVersion;
    setRelatedSource(item);
    setRelatedItems([]);
    setRelatedLoading(true);
    api
      .similar(item.item_id)
      .then((result) => {
        if (version === relatedVersion) setRelatedItems(result.items ?? []);
      })
      .catch((caught) => {
        if (version === relatedVersion) handleError(caught);
      })
      .finally(() => {
        if (version === relatedVersion) setRelatedLoading(false);
      });
  };

  const closeRelated = () => {
    if (!relatedSource()) return;
    closeOverlay("related");
    relatedVersion++;
    setRelatedSource();
    setRelatedItems([]);
    setRelatedLoading(false);
  };

  const finishReaderClose = () => {
    window.clearTimeout(readerCloseTimer);
    readerCloseTimer = undefined;
    setReaderID("");
    setReaderItem();
    setReaderClosing(false);
    setReaderReveal(0);
    setReaderDragging(false);
  };

  const beginReaderClose = (popHistory: boolean) => {
    if (!readerID() || readerClosing()) return;
    if (popHistory) closeOverlay("reader");
    setReaderClosing(true);
    setReaderReveal(1);
    setReaderDragging(false);
    const duration = window.matchMedia("(prefers-reduced-motion: reduce)")
      .matches
      ? 0
      : READER_EXIT_MS;
    readerCloseTimer = window.setTimeout(finishReaderClose, duration);
  };

  const closeReader = () => beginReaderClose(true);

  const openReaderHistory = () => {
    if (readerID()) return;
    window.clearTimeout(readerCloseTimer);
    setReaderClosing(false);
    setReaderReveal(0);
    setReaderDragging(false);
    pushOverlay("reader", () => beginReaderClose(false));
  };

  const closeConfirmRemove = () => {
    if (!confirmRemove()) return;
    closeOverlay("confirm-remove");
    setConfirmRemove();
  };

  const setSignal = (item: Item, value: -1 | 0 | 1) => {
    const previous = item.signal;
    const effective = item.hearted && value === 0 ? 1 : value;
    replaceItem(item.item_id, { signal: effective });
    api.signal(item.item_id, value).catch((caught) => {
      replaceItem(item.item_id, { signal: previous });
      handleError(caught);
    });
  };

  const performHeart = async (item: Item) => {
    if (heartsInFlight.has(item.item_id)) return;
    heartsInFlight.add(item.item_id);
    const previous = item.hearted;
    const next = !previous;
    replaceItem(item.item_id, { hearted: next });
    try {
      const result = await api.heart(item.item_id, next);
      setHeartCount(result.heart_count);
      setProfile((current) =>
        current ? { ...current, heart_count: result.heart_count } : current,
      );
      if (mode() === "archive" && !next) {
        const remainingIDs = gridIDs().filter((id) => id !== item.item_id);
        setItems((current) =>
          current.filter((candidate) => candidate.item_id !== item.item_id),
        );
        setGridIDs(remainingIDs);
        if (readerID() === item.item_id) closeReader();
        setFocusedID((current) =>
          current === item.item_id ? (remainingIDs[0] ?? "") : current,
        );
        setLayoutVersion((value) => value + 1);
        showToast("success", "Removed from archive");
      }
      if (item.archived && !next) {
        setSearchResponse((current) =>
          current ? removeSearchItem(current, item.item_id) : current,
        );
        setRelatedItems((current) =>
          current.filter((candidate) => candidate.item_id !== item.item_id),
        );
      }
    } catch (caught) {
      replaceItem(item.item_id, { hearted: previous });
      if (caught instanceof UnauthorizedError) props.signOut();
      else
        showToast(
          "error",
          next ? "Couldn’t keep this item" : "Couldn’t remove this item",
        );
    } finally {
      heartsInFlight.delete(item.item_id);
    }
  };

  const toggleHeart = (item: Item) => {
    if (
      shouldConfirmArchiveRemoval(
        mode() === "archive" || item.archived === true,
        item.hearted,
      )
    ) {
      if (!confirmRemove())
        pushOverlay("confirm-remove", () => setConfirmRemove());
      setConfirmRemove(item);
      return;
    }
    void performHeart(item);
  };

  const writeReadBatch = (ids: string[], read: boolean, keepalive = false) =>
    Promise.all(
      Array.from({ length: Math.ceil(ids.length / 100) }, (_, index) =>
        api.readBatch(
          ids.slice(index * 100, (index + 1) * 100),
          read,
          keepalive,
        ),
      ),
    );

  const queueRead = (ids: string[], gridSnapshot?: GridClearSnapshot) => {
    if (mode() === "archive") return;
    const requested = new Set(ids);
    const alreadyRead = new Set(
      [...items(), ...stories().flatMap((story) => story.items)]
        .filter((item) => item.read)
        .map((item) => item.item_id),
    );
    const unread = [...requested].filter((id) => !alreadyRead.has(id));
    if (unread.length === 0) return;
    setReadAdjust((value) => value + unread.length);
    for (const id of unread) pendingRead.add(id);
    if (!gridSnapshot) clearUndoTimer();
    setUndo({
      ids: [...pendingRead],
      gridSnapshot: gridSnapshot
        ? { ...gridSnapshot, count: unread.length }
        : undefined,
    });
    if (gridSnapshot) startFinishUndoTimer();
    setItems((current) => updateRead(current, requested, true));
    setStories((current) => updateStoriesRead(current, requested, true));
    if (document.visibilityState === "hidden") {
      void flushRead(true);
      return unread;
    }
    window.clearTimeout(readTimer);
    readTimer = window.setTimeout(() => void flushPending(), 5_000);
    return unread;
  };

  const flushRead = (keepalive = false) => {
    window.clearTimeout(readTimer);
    readTimer = undefined;
    const ids = [...pendingRead];
    if (ids.length === 0) return Promise.resolve();
    setUndo((current) => (current?.gridSnapshot ? current : { ids }));
    const operation = pendingRead
      .flush((batch) => api.readBatch(batch, true, keepalive))
      .catch((caught) => {
        handleError(caught);
        if (!disposed && pendingRead.size > 0 && readTimer === undefined) {
          readTimer = window.setTimeout(() => void flushPending(), 5_000);
        }
      })
      .finally(() => readFlushes.delete(operation));
    readFlushes.add(operation);
    return operation;
  };

  const queueEvent = (itemID: string, event: BehaviourEvent) => {
    const current = pendingEvents.get(itemID) ?? {};
    pendingEvents.set(itemID, mergeBehaviourEvent(current, event));
    window.clearTimeout(readTimer);
    readTimer = window.setTimeout(() => void flushPending(), 5_000);
  };

  const flushEvents = (keepalive = false) => {
    const events = [...pendingEvents.entries()];
    pendingEvents.clear();
    return Promise.all(
      events.map(([itemID, event]) => api.events(itemID, event, keepalive)),
    ).catch(handleError);
  };

  const flushPending = (keepalive = false) => {
    window.clearTimeout(readTimer);
    readTimer = undefined;
    return Promise.all([flushRead(keepalive), flushEvents(keepalive)]);
  };

  const recordOpened = (item: Item, archive = item.archived === true) => {
    if (!archive) api.events(item.item_id, { opened: true }).catch(handleError);
    if (!archive && !item.read) {
      setReadAdjust((value) => value + 1);
      replaceItem(item.item_id, { read: true });
      api.read(item.item_id, true).catch((caught) => {
        setReadAdjust((value) => value - 1);
        replaceItem(item.item_id, { read: false });
        handleError(caught);
      });
    }
  };

  const markOpened = (item: Item, archive = item.archived === true) => {
    openReaderHistory();
    setReaderItem(item);
    recordOpened(item, archive);
    setReaderArchive(archive);
    setReaderID(item.item_id);
  };

  const markStoryOpened = (story: Story) => {
    const lead = story.items[0];
    if (!lead) return;
    const ids = story.items.map((item) => item.item_id);
    const unread = story.items
      .filter((item) => !item.read)
      .map((item) => item.item_id);
    openReaderHistory();
    for (const id of ids) pendingRead.delete(id);
    setReaderItem({ ...lead, read: true });
    setReaderArchive(false);
    setReaderID(lead.item_id);

    let readUpdateApplied = false;
    let readFailed = false;
    // Let the reader paint before updating the much larger front-page tree.
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        if (readFailed) return;
        readUpdateApplied = true;
        setReadAdjust((value) => value + unread.length);
        setStories((current) => updateStoriesRead(current, ids, true));
        setItems((current) => updateRead(current, ids, true));
      });
    });
    api.events(lead.item_id, { opened: true }).catch(handleError);
    api.readBatch(ids, true).catch((caught) => {
      readFailed = true;
      if (readUpdateApplied) {
        setReadAdjust((value) => value - unread.length);
        setStories((current) => updateStoriesRead(current, unread, false));
        setItems((current) => updateRead(current, unread, false));
      }
      setReaderItem((current) =>
        current?.item_id === lead.item_id
          ? { ...current, read: lead.read }
          : current,
      );
      handleError(caught);
    });
  };

  const openExternalItem = (item: Item) => {
    recordOpened(item);
    if (!item.archived) queueEvent(item.item_id, { clicked_through: true });
  };

  const recordClickThrough = (item: Item) => {
    if (!item.archived) queueEvent(item.item_id, { clicked_through: true });
  };

  const openOriginal = (item: Item) => {
    if (!item.archived) queueEvent(item.item_id, { clicked_through: true });
    window.open(item.url, "_blank", "noopener,noreferrer");
  };

  const toggleRead = (item: Item) => {
    if (mode() === "archive") return;
    pendingRead.delete(item.item_id);
    if (pendingRead.size === 0) {
      window.clearTimeout(readTimer);
      readTimer = undefined;
    }
    setReadAdjust((value) => value + (item.read ? -1 : 1));
    replaceItem(item.item_id, { read: !item.read });
    api.read(item.item_id, !item.read).catch((caught) => {
      setReadAdjust((value) => value - (item.read ? -1 : 1));
      replaceItem(item.item_id, { read: item.read });
      handleError(caught);
    });
  };

  const toggleStoryRead = (story: Story) => {
    if (mode() === "archive") return;
    const read = story.items.some((item) => !item.read);
    const adjustment =
      story.items.filter((item) => item.read !== read).length * (read ? 1 : -1);
    setReadAdjust((value) => value + adjustment);

    const ids = story.items.map((item) => item.item_id);
    for (const id of ids) pendingRead.delete(id);
    setStories((current) => updateStoriesRead(current, ids, read));
    setItems((current) => updateRead(current, ids, read));
    api.readBatch(ids, read).catch((caught) => {
      setReadAdjust((value) => value - adjustment);
      setStories((current) =>
        current.map((currentStory) =>
          currentStory.story_id !== story.story_id
            ? currentStory
            : { ...currentStory, items: story.items },
        ),
      );
      setItems((current) =>
        current.map((item) => {
          const previous = story.items.find(
            (member) => member.item_id === item.item_id,
          );
          return previous ? { ...item, read: previous.read } : item;
        }),
      );
      handleError(caught);
    });
  };

  function undoLast() {
    const operation = undo();
    if (!operation) return;
    setReadAdjust((value) => value - operation.ids.length);
    clearUndoTimer();
    undoRemaining = 8_000;
    setUndo(undefined);
    const unsent = new Set(
      operation.ids.filter((id) => pendingRead.delete(id)),
    );
    if (pendingRead.size === 0) {
      window.clearTimeout(readTimer);
      readTimer = undefined;
    }
    setItems((current) => updateRead(current, operation.ids, false));
    setStories((current) => updateStoriesRead(current, operation.ids, false));
    if (operation.gridSnapshot && mode() === "live" && unreadOnly()) {
      gridClearVersion++;
      gridScrollTop = operation.gridSnapshot.scrollTop;
      setGridIDs([...operation.gridSnapshot.ids]);
      setGridStoryIDs([...(operation.gridSnapshot.storyIDs ?? [])]);
      setFocusedID(operation.gridSnapshot.focusedID);
      setScrollTarget(operation.gridSnapshot.scrollTop);
      setLayoutVersion((value) => value + 1);
      setScrollTopVersion((value) => value + 1);
    }
    const sent = operation.ids.filter((id) => !unsent.has(id));
    if (sent.length > 0) writeReadBatch(sent, false).catch(handleError);
  }

  const finishAndClear = (ids: string[]) => {
    if (mode() !== "live" || !unreadOnly()) return;
    const cleared = finishAndClearGrid(gridIDs(), focusedID(), gridScrollTop);
    cleared.snapshot.storyIDs = [...gridStoryIDs()];
    const queued =
      ids.length > 0 ? queueRead(ids, cleared.snapshot) : undefined;
    if (!queued || queued.length === 0) {
      setUndo({
        ids: [],
        gridSnapshot: { ...cleared.snapshot, count: 0 },
      });
      startFinishUndoTimer();
    }
    gridClearVersion++;
    gridScrollTop = 0;
    setGridIDs(cleared.ids);
    setGridStoryIDs([]);
    setFocusedID("");
    setScrollTarget(0);
    setLayoutVersion((value) => value + 1);
    setScrollTopVersion((value) => value + 1);
  };

  const markBelow = async (item: Item) => {
    if (mode() === "archive" || markBelowInFlight) return;
    markBelowInFlight = true;
    const version = requestVersion;
    const clearVersion = gridClearVersion;
    const markOrder = gridOrder();
    const ids =
      markOrder === "interest"
        ? frontPageUnreadIDsAfter(
            mergeFrontPage(stories(), items(), false),
            item.item_id,
          )
        : unreadIDsAfter(items(), item.item_id);
    let nextCursor = cursor();
    try {
      while (nextCursor !== "") {
        const page = await api.items(
          markOrder,
          nextCursor,
          includeReadForGrid(unreadOnly()),
          scope(),
          false,
          fetchWindow,
        );
        if (
          version !== requestVersion ||
          clearVersion !== gridClearVersion ||
          markOrder !== gridOrder()
        )
          return;
        const pageItems =
          markOrder === "interest"
            ? excludeRenderedStoryItems(page.items ?? [], stories())
            : (page.items ?? []);
        ids.push(
          ...pageItems
            .filter((candidate) => !candidate.read)
            .map((candidate) => candidate.item_id),
        );
        nextCursor = page.next_cursor ?? "";
      }
      if (clearVersion === gridClearVersion) queueRead(ids);
    } catch (caught) {
      handleError(caught);
    } finally {
      markBelowInFlight = false;
    }
  };

  const markStoryBelow = async (storyID: string) => {
    if (mode() === "archive" || markBelowInFlight) return;
    if (!stories().some((story) => story.story_id === storyID)) return;
    markBelowInFlight = true;
    const version = requestVersion;
    const clearVersion = gridClearVersion;
    const markOrder = gridOrder();
    const ids = frontPageUnreadIDsAfter(
      mergeFrontPage(stories(), items(), false),
      `story:${storyID}`,
    );
    let nextCursor = cursor();
    try {
      while (nextCursor !== "") {
        const page = await api.items(
          markOrder,
          nextCursor,
          includeReadForGrid(unreadOnly()),
          scope(),
          false,
          fetchWindow,
        );
        if (
          version !== requestVersion ||
          clearVersion !== gridClearVersion ||
          markOrder !== gridOrder()
        )
          return;
        const pageItems =
          markOrder === "interest"
            ? excludeRenderedStoryItems(page.items ?? [], stories())
            : (page.items ?? []);
        ids.push(
          ...pageItems.filter((item) => !item.read).map((item) => item.item_id),
        );
        nextCursor = page.next_cursor ?? "";
      }
      if (clearVersion === gridClearVersion) queueRead(ids);
    } catch (caught) {
      handleError(caught);
    } finally {
      markBelowInFlight = false;
    }
  };

  const selectItemView = async (next: ItemView) => {
    if (mode() === "archive" || next === itemView()) return;
    await flushRead();
    setItemView(next);
    void reload();
  };

  const toggleUnread = () => selectItemView(unreadOnly() ? "all" : "unread");

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
    void refreshFeedItemCounts();
    return added.length;
  };

  const insertPendingNew = () =>
    gridOrder() === "interest" ? pollNew(true) : insertNewItems(pendingNew());

  const selectOrder = async (next: Order) => {
    if (mode() === "archive" || feedScoped() || next === order()) return;
    await flushRead();
    setOrder(next);
    closeReader();
    setProfile((current) =>
      current ? { ...current, order_pref: next } : current,
    );
    api.patchMe({ order_pref: next }).catch(handleError);
    await reload(next);
  };

  const toggleOrder = () =>
    selectOrder(order() === "chrono" ? "interest" : "chrono");

  const applyTag = async (tag: string) => {
    const nextScope: GridScope = tag ? { kind: "tag", value: tag } : null;
    if (sameGridScope(scope(), nextScope)) return;
    await flushRead();
    setScope(nextScope);
    closeReader();
    setProfile((current) =>
      current
        ? {
            ...current,
            tag_pref: tag,
            ...(tag ? { feed_pref: "" } : {}),
          }
        : current,
    );
    api.patchMe({ tag_pref: tag }).catch(handleError);
    await reload(order(), unreadOnly(), mode(), nextScope);
  };

  const applyFeed = async (feedID: string) => {
    const nextScope: GridScope = feedID
      ? { kind: "feed", value: feedID }
      : null;
    if (sameGridScope(scope(), nextScope)) return;
    await flushRead();
    setScope(nextScope);
    closeReader();
    setProfile((current) =>
      current
        ? {
            ...current,
            feed_pref: feedID,
            ...(feedID ? { tag_pref: "" } : {}),
          }
        : current,
    );
    api.patchMe({ feed_pref: feedID }).catch(handleError);
    await reload(order(), unreadOnly(), mode(), nextScope);
  };

  const applyScope = (nextScope: GridScope) => {
    if (nextScope?.kind === "tag") return applyTag(nextScope.value);
    if (nextScope?.kind === "feed") return applyFeed(nextScope.value);
    return scope()?.kind === "feed" ? applyFeed("") : applyTag("");
  };

  const refreshFeedItemCounts = async () => {
    const version = ++feedItemCountVersion;
    const window = itemViewWindow(itemView());
    try {
      // Counts must include queued and already-in-flight read writes before
      // they replace the optimistic adjustment (including finish-and-clear).
      await flushRead();
      await Promise.all(readFlushes);
      if (version !== feedItemCountVersion || pendingRead.size > 0) return;
      const latest = await api.feedItemCounts(window);
      if (version === feedItemCountVersion)
        batch(() => {
          setFeedItemCounts(latest ?? {});
          setScopeCountsWindowKey(window?.from ?? "");
          setReadAdjust(0);
        });
    } catch (caught) {
      handleError(caught);
    }
  };

  const refreshFeedFilters = async () => {
    try {
      const [latest] = await Promise.all([
        api.feeds(),
        refreshFeedItemCounts(),
      ]);
      setFeedFilters(latest);
      const activeScope = scope();
      if (
        activeScope?.kind === "tag" &&
        activeScope.value !== "untagged" &&
        !latest.some((feed) => feed.tags?.includes(activeScope.value))
      ) {
        setScope(null);
        setProfile((current) =>
          current ? { ...current, tag_pref: "" } : current,
        );
        api.patchMe({ tag_pref: "" }).catch(handleError);
      } else if (
        activeScope?.kind === "feed" &&
        !latest.some(
          (feed) => feed.feed_id === activeScope.value && !feed.muted,
        )
      ) {
        setScope(null);
        setProfile((current) =>
          current ? { ...current, feed_pref: "" } : current,
        );
        api.patchMe({ feed_pref: "" }).catch(handleError);
      }
    } catch (caught) {
      handleError(caught);
    }
  };

  const noteFeedsChanged = () => {
    feedsGridDirty = true;
    const refresh = refreshFeedFilters();
    feedFilterRefresh = refresh;
    void refresh.finally(() => {
      if (feedFilterRefresh === refresh) feedFilterRefresh = undefined;
    });
  };

  const backToTop = async () => {
    const wasFeeds = view() === "feeds";
    const wasArchive = mode() === "archive";
    setHeaderMenu();
    closeKeys();
    closeRelated();
    closeReader();
    setReaderArchive(false);
    clearSearch();
    setMode("live");
    if (wasFeeds) closeOverlay("feeds");
    setView("grid");
    setScrollTarget(0);
    setScrollTopVersion((value) => value + 1);
    if (wasFeeds) await refreshFeedFilters();
    if (wasFeeds || wasArchive)
      await reload(order(), unreadOnly(), "live", scope());
  };

  const openFeedsAndSettings = () => {
    clearGo();
    setHeaderMenu();
    closeKeys();
    closeRelated();
    closeReader();
    clearSearch();
    if (view() !== "feeds")
      pushOverlay("feeds", () => void leaveFeedsAndSettings());
    setView("feeds");
  };

  const leaveFeedsAndSettings = async () => {
    clearGo();
    closeKeys();
    setView("grid");
    if (!feedsGridDirty) return;
    feedsGridDirty = false;
    await feedFilterRefresh;
    await reload(order(), unreadOnly(), mode(), scope());
  };

  const closeFeedsAndSettings = async () => {
    if (view() === "feeds") closeOverlay("feeds");
    await leaveFeedsAndSettings();
  };

  const toggleArchive = async () => {
    await flushRead();
    const next = mode() === "live" ? "archive" : "live";
    setMode(next);
    setView("grid");
    closeReader();
    closeConfirmRemove();
    await reload(order(), unreadOnly(), next);
  };

  const navigateByKey = async (command: GoCommand) => {
    if (command === "settings") {
      if (view() === "feeds") await closeFeedsAndSettings();
      else openFeedsAndSettings();
      return;
    }
    if (view() === "feeds") await closeFeedsAndSettings();
    if (command === "archive") {
      if (mode() !== "archive") await toggleArchive();
      return;
    }
    if (mode() === "archive") {
      await flushRead();
      setItemView(command);
      setMode("live");
      await reload();
    } else {
      await selectItemView(command);
    }
  };

  const changeCharacterShortcuts = (enabled: boolean) => {
    clearGo();
    setCharacterShortcuts(enabled);
    try {
      localStorage.setItem("sema:character-shortcuts", enabled ? "on" : "off");
    } catch {
      /* The preference still applies for this session. */
    }
  };

  const moveReader = (delta: number) => {
    const next = frontPageItems()[selectedIndex() + delta];
    if (!next) return;
    const story = stories().find(
      (candidate) => candidate.items[0]?.item_id === next.item_id,
    );
    if (story) markStoryOpened(story);
    else markOpened(next);
  };

  return (
    <Show
      when={view() === "grid"}
      fallback={
        <>
          <Feeds
            api={api}
            heartCount={heartCount()}
            onBack={() => void closeFeedsAndSettings()}
            onKeys={openKeys}
            characterShortcuts={characterShortcuts()}
            onCharacterShortcuts={changeCharacterShortcuts}
            onSignOut={props.signOut}
            onFeedsChanged={noteFeedsChanged}
            onToast={showToast}
          />
          <Show when={keysOpen()}>
            <KeyboardMap
              onClose={closeKeys}
              characterShortcuts={characterShortcuts()}
              onCharacterShortcuts={changeCharacterShortcuts}
            />
          </Show>
        </>
      }
    >
      <main
        class="app-shell"
        classList={{
          searching: searchActive(),
          "search-focused": searchFocused(),
          "search-open": searchOpen(),
          "tag-filter-open": tagFilterOpen(),
        }}
      >
        <AppHeader
          view="grid"
          onHome={() => void backToTop()}
          tooltipDisabled={headerTooltipDisabled()}
        >
          <Show when={mode() === "live"}>
            <div class="header-display-controls">
              <div class="header-segments">
                <div
                  class="segmented segmented-control"
                  role="radiogroup"
                  aria-label="Item order"
                  aria-disabled={feedScoped()}
                  title={
                    feedScoped()
                      ? "Newest first while filtering by feed"
                      : undefined
                  }
                >
                  <button
                    type="button"
                    class="segmented__item"
                    classList={{ active: gridOrder() === "interest" }}
                    role="radio"
                    aria-checked={gridOrder() === "interest"}
                    disabled={feedScoped()}
                    title="Toggle order (t)"
                    onClick={() => void selectOrder("interest")}
                  >
                    <span>Front page</span>
                  </button>
                  <button
                    type="button"
                    class="segmented__item"
                    classList={{ active: gridOrder() === "chrono" }}
                    role="radio"
                    aria-checked={gridOrder() === "chrono"}
                    disabled={feedScoped()}
                    title="Toggle order (t)"
                    onClick={() => void selectOrder("chrono")}
                  >
                    <span>Latest</span>
                  </button>
                </div>
                <div
                  class="segmented segmented-control"
                  role="radiogroup"
                  aria-label="Items shown"
                >
                  <For each={ITEM_VIEWS}>
                    {(option) => (
                      <button
                        type="button"
                        class="segmented__item"
                        classList={{ active: itemView() === option.value }}
                        role="radio"
                        aria-checked={itemView() === option.value}
                        title={`${option.label} (${scopeShortcuts[option.value]})`}
                        onClick={() => void selectItemView(option.value)}
                      >
                        <span>{option.label}</span>
                      </button>
                    )}
                  </For>
                </div>
              </div>
              <button
                type="button"
                class="chrome-btn filter-button"
                classList={{ "is-hidden": !compactDisplayControls() }}
                aria-haspopup="dialog"
                aria-expanded={headerMenu() === "combined"}
                onClick={() =>
                  setHeaderMenu((current) =>
                    current === "combined" ? undefined : "combined",
                  )
                }
              >
                <span>{orderLabel()}</span>
                <Icon name="chevron-down" size={13} />
              </button>
            </div>
          </Show>

          <div class="header-spacer" />
          <div class="chrome-group chrome-group--icons header-tools">
            <TagFilter
              feeds={feedFilters()}
              itemCounts={feedItemCounts()}
              unreadOnly={unreadOnly()}
              value={scope()}
              active={
                !readerID() && !keysOpen() && !confirmRemove() && !headerMenu()
              }
              openRequest={tagOpenRequest()}
              tooltipDisabled={headerTooltipDisabled()}
              onOpenChange={(open) => {
                setTagFilterOpen(open);
                if (open && mode() === "live")
                  void flushRead().then(refreshFeedItemCounts);
              }}
              onChange={(nextScope) => void applyScope(nextScope)}
            />
            <div class="search-slot" classList={{ open: searchOpen() }}>
              <Show
                when={searchOpen()}
                fallback={
                  <Tooltip
                    name="Search"
                    shortcut="/"
                    disabled={headerTooltipDisabled()}
                  >
                    <button
                      type="button"
                      class="chrome-icon header-icon-button search-trigger"
                      aria-label="Search"
                      onClick={focusSearch}
                    >
                      <Icon name="search" size={18} />
                    </button>
                  </Tooltip>
                }
              >
                <label
                  class="search-field"
                  classList={{
                    focused: searchFocused(),
                    typing: searchQuery().length > 0,
                  }}
                >
                  <Show
                    when={!searchLoading()}
                    fallback={<i class="search-ring" />}
                  >
                    <Icon name="search" size={14} />
                  </Show>
                  <input
                    ref={searchInput}
                    type="search"
                    value={searchQuery()}
                    placeholder="Search or describe a topic"
                    aria-label="Search window and archive"
                    onFocus={() => setSearchFocused(true)}
                    onBlur={() => setSearchFocused(false)}
                    onInput={(event) =>
                      setSearchQuery(event.currentTarget.value)
                    }
                    onKeyDown={(event) => {
                      if (event.key === "Escape") {
                        event.preventDefault();
                        event.stopPropagation();
                        clearSearch();
                      }
                    }}
                  />
                  <Show when={searchQuery()} fallback={<kbd>esc</kbd>}>
                    <button
                      type="button"
                      aria-label="Clear search"
                      onClick={clearSearch}
                    >
                      <Icon name="close" size={14} />
                    </button>
                  </Show>
                </label>
              </Show>
            </div>
            <Tooltip
              name="Archive"
              shortcut="g → r · Shift+a"
              disabled={headerTooltipDisabled()}
            >
              <button
                type="button"
                class="chrome-icon header-icon-button archive-toggle"
                classList={{ active: mode() === "archive" }}
                aria-pressed={mode() === "archive"}
                aria-label="Archive"
                onClick={() => void toggleArchive()}
              >
                <Icon name="archive" size={18} />
              </button>
            </Tooltip>
          </div>
          <span
            class="chrome-divider header-tools-divider"
            aria-hidden="true"
          />
          <ThemeToggle
            theme={props.theme}
            tooltipDisabled={headerTooltipDisabled()}
          />
          <Tooltip
            name="Feeds & settings"
            shortcut="g → s"
            disabled={headerTooltipDisabled()}
            align="end"
          >
            <button
              type="button"
              class="chrome-icon header-icon-button settings-trigger"
              aria-label="Feeds & settings"
              onClick={openFeedsAndSettings}
            >
              <Icon name="settings" size={18} />
            </button>
          </Tooltip>
          <button
            type="button"
            class="chrome-btn chrome-btn--icon grid-overflow-trigger"
            classList={{ "is-hidden": !phoneHeader() }}
            aria-label="More"
            aria-haspopup="dialog"
            aria-expanded={headerMenu() === "overflow"}
            onClick={() =>
              setHeaderMenu((current) =>
                current === "overflow" ? undefined : "overflow",
              )
            }
          >
            <Icon name="menu" size={18} />
          </button>
        </AppHeader>
        <Show when={headerMenu() === "combined"}>
          <div class="header-sheet-layer">
            <button
              type="button"
              class="header-sheet-backdrop"
              aria-label="Close feed view menu"
              onClick={() => setHeaderMenu()}
            />
            <section
              class="header-sheet"
              role="dialog"
              aria-modal="true"
              aria-label="Feed view"
            >
              <div class="header-sheet-row">
                <span>Order</span>
                <div
                  role="radiogroup"
                  aria-label="Item order"
                  aria-disabled={feedScoped()}
                  title={
                    feedScoped()
                      ? "Newest first while filtering by feed"
                      : undefined
                  }
                >
                  <button
                    type="button"
                    role="radio"
                    aria-checked={gridOrder() === "interest"}
                    classList={{ active: gridOrder() === "interest" }}
                    disabled={feedScoped()}
                    title="Toggle order (t)"
                    onClick={() => void selectOrder("interest")}
                  >
                    Front page
                  </button>
                  <button
                    type="button"
                    role="radio"
                    aria-checked={gridOrder() === "chrono"}
                    classList={{ active: gridOrder() === "chrono" }}
                    disabled={feedScoped()}
                    title="Toggle order (t)"
                    onClick={() => void selectOrder("chrono")}
                  >
                    Latest
                  </button>
                </div>
              </div>
              <div class="header-sheet-row">
                <span>Items</span>
                <div role="radiogroup" aria-label="Items shown">
                  <For each={ITEM_VIEWS}>
                    {(option) => (
                      <button
                        type="button"
                        role="radio"
                        aria-checked={itemView() === option.value}
                        title={`${option.label} (${scopeShortcuts[option.value]})`}
                        classList={{ active: itemView() === option.value }}
                        onClick={() => void selectItemView(option.value)}
                      >
                        {option.label}
                      </button>
                    )}
                  </For>
                </div>
              </div>
            </section>
          </div>
        </Show>
        <Show when={headerMenu() === "overflow"}>
          <div class="header-sheet-layer">
            <button
              type="button"
              class="header-sheet-backdrop"
              aria-label="Close more options"
              onClick={() => setHeaderMenu()}
            />
            <section
              class="header-sheet header-overflow-sheet"
              role="dialog"
              aria-modal="true"
              aria-label="More options"
            >
              <button
                type="button"
                onClick={() => {
                  setHeaderMenu();
                  setTagOpenRequest((value) => value + 1);
                }}
              >
                <Icon name="tag" size={18} />
                <span>Filter by tag or feed</span>
                <Show when={scope()}>
                  {(activeScope) => (
                    <span class="mobile-active-tag">
                      {activeScope().kind === "tag"
                        ? `#${activeScope().value}`
                        : activeFeedTitle()}
                    </span>
                  )}
                </Show>
              </button>
              <button
                type="button"
                classList={{ active: mode() === "archive" }}
                aria-pressed={mode() === "archive"}
                onClick={() => {
                  setHeaderMenu();
                  void toggleArchive();
                }}
              >
                <Icon name="archive" size={18} />
                <span>Archive</span>
              </button>
              <button
                type="button"
                aria-label={`Theme: ${props.theme.preference()} — switch to ${nextThemePreference(props.theme.preference())}`}
                onClick={props.theme.cyclePreference}
              >
                <Icon
                  name={
                    props.theme.preference() === "system"
                      ? "theme-system"
                      : props.theme.preference() === "light"
                        ? "theme-light"
                        : "theme-dark"
                  }
                  size={18}
                />
                <span>Theme: {props.theme.preference()}</span>
                <kbd>next</kbd>
              </button>
              <button type="button" onClick={openFeedsAndSettings}>
                <Icon name="settings" size={18} />
                <span>Feeds &amp; settings</span>
                <kbd>G S</kbd>
              </button>
            </section>
          </div>
        </Show>
        <Show
          when={
            mode() === "live" &&
            pendingNew().length > 0 &&
            !searchActive() &&
            !readerID() &&
            !relatedSource()
          }
        >
          <button
            type="button"
            class="new-items-pill"
            classList={{ "new-items-pill--scope": Boolean(scopeCell()) }}
            onClick={insertPendingNew}
          >
            {pendingNew().length} new
          </button>
        </Show>
        <Show when={error()}>
          <div class="error-banner" role="alert">
            <span>{error()}</span>
            <button
              type="button"
              aria-label="Dismiss error"
              onClick={() => setError("")}
            >
              <Icon name="close" />
            </button>
          </div>
        </Show>
        <Show
          when={!loading() || items().length > 0 || stories().length > 0}
          fallback={
            <div class="loading-screen">
              <i />
              <span>
                {mode() === "archive"
                  ? "Loading your archive…"
                  : "Loading your feed…"}
              </span>
            </div>
          }
        >
          <Show
            when={
              items().length > 0 ||
              stories().length > 0 ||
              Boolean(readAnchor()) ||
              (mode() === "live" &&
                (feedFilters().length > 0 ||
                  Boolean(scope()) ||
                  itemView() !== "unread"))
            }
            fallback={
              scope() ? (
                <FilteredEmpty
                  scope={scope()}
                  archive={mode() === "archive"}
                  feedTitle={activeFeedTitle()}
                  onClear={() => void applyScope(null)}
                />
              ) : mode() === "archive" ? (
                <ArchiveEmpty />
              ) : (
                <ColdStart onImport={openFeedsAndSettings} />
              )
            }
          >
            <Grid
              items={gridItems()}
              entries={frontPageEntries()}
              stories={gridStories()}
              expandedStoryIDs={expandedStoryIDs()}
              layoutKey={layoutVersion()}
              scrollToTopKey={scrollTopVersion()}
              scrollTarget={scrollTarget()}
              initialScrollTop={gridScrollTop}
              focusedID={focusedID()}
              active={
                view() === "grid" &&
                !readerID() &&
                !keysOpen() &&
                !confirmRemove() &&
                !searchActive() &&
                !relatedSource()
              }
              readerOpen={Boolean(readerID())}
              readerReveal={readerReveal()}
              readerDragging={readerDragging()}
              hasMore={cursor() !== ""}
              scopeCell={scopeCell()}
              scope={scope()}
              itemView={itemView()}
              onClearScope={() =>
                scope()?.kind === "feed"
                  ? void applyFeed("")
                  : void applyTag("")
              }
              onShowAll={() => void selectItemView("all")}
              onShowRead={() => void selectItemView("all")}
              onOpenArchive={() => void navigateByKey("archive")}
              onSelectView={(view) => void selectItemView(view)}
              clearedCount={finishUndo()?.count}
              archive={mode() === "archive"}
              unreadOnly={unreadOnly()}
              order={gridOrder()}
              readStateItems={items()}
              readAnchor={readAnchor()}
              linkActionID={linkActionID()}
              pendingNewCount={pendingNew().length}
              onFocus={setFocusedID}
              onOpen={markOpened}
              onOpenStoryLead={markStoryOpened}
              onExternalOpen={openExternalItem}
              onDiscussion={recordClickThrough}
              onSignal={setSignal}
              onHeart={toggleHeart}
              onToggleRead={toggleRead}
              onToggleStoryRead={toggleStoryRead}
              onCopy={copyLink}
              onOriginal={openOriginal}
              onRelated={openRelated}
              onApplyFeed={(item) => void applyFeed(item.feed_id)}
              onMarkBelow={markBelow}
              onMarkStoryBelow={markStoryBelow}
              onExpandStory={(storyID) =>
                setExpandedStoryIDs((current) => {
                  const next = new Set(current);
                  if (next.has(storyID)) next.delete(storyID);
                  else next.add(storyID);
                  return next;
                })
              }
              onItemsPassed={queueRead}
              onFinishAndClear={finishAndClear}
              onLoadMore={loadMore}
              onToggleOrder={toggleOrder}
              onUndo={undoLast}
              onRefresh={() => pollNew(true)}
              onScrollPosition={(top) => {
                gridScrollTop = top;
              }}
            />
          </Show>
        </Show>
        <Show when={loadingMore()}>
          <div class="page-loader">fetching more…</div>
        </Show>
        <Show when={searchActive()}>
          <SearchResults
            query={searchQuery().trim()}
            response={searchResponse()}
            loading={searchLoading()}
            focusedID={searchFocusedID()}
            active={
              !readerID() && !relatedSource() && !keysOpen() && !confirmRemove()
            }
            linkActionID={linkActionID()}
            onFocus={setSearchFocusedID}
            onOpen={(item, archive) => markOpened(item, archive)}
            onExternalOpen={openExternalItem}
            onDiscussion={recordClickThrough}
            onSignal={setSignal}
            onHeart={toggleHeart}
            onCopy={copyLink}
            onRelated={openRelated}
            onEscape={clearSearch}
          />
        </Show>
        <Show when={selected()}>
          {(item) => (
            <Reader
              item={item()}
              active={!keysOpen() && !confirmRemove() && !relatedSource()}
              archive={readerArchive()}
              hearted={item().hearted}
              linkActionActive={linkActionID() === item().item_id}
              canPrevious={selectedIndex() > 0}
              canNext={
                selectedIndex() >= 0 &&
                selectedIndex() < frontPageItems().length - 1
              }
              closing={readerClosing()}
              onClose={closeReader}
              onReveal={(progress, dragging) => {
                setReaderReveal(progress);
                setReaderDragging(dragging);
              }}
              onHome={() => void backToTop()}
              onPrevious={() => moveReader(-1)}
              onNext={() => moveReader(1)}
              onSignal={(value) => setSignal(item(), value)}
              onHeart={() => toggleHeart(item())}
              onCopy={() => copyLink(item())}
              onOriginal={() =>
                !readerArchive() &&
                queueEvent(item().item_id, { clicked_through: true })
              }
              onRelated={() => openRelated(item())}
              onApplyFeed={() => void applyFeed(item().feed_id)}
              onRetry={() => {
                api
                  .retryItem(item().item_id)
                  .then(() => showToast("success", "Extraction queued"))
                  .catch(handleError);
              }}
              onDwell={(itemID, dwellMS) =>
                !readerArchive() && queueEvent(itemID, { dwell_ms: dwellMS })
              }
            />
          )}
        </Show>
        <Show when={relatedSource()}>
          {(source) => (
            <RelatedPanel
              source={source()}
              items={relatedItems()}
              loading={relatedLoading()}
              active={!keysOpen() && !confirmRemove()}
              linkActionID={linkActionID()}
              onClose={closeRelated}
              onWalk={openRelated}
              onOpen={(item) => {
                closeRelated();
                markOpened(item, item.archived === true);
              }}
              onExternalOpen={openExternalItem}
              onDiscussion={recordClickThrough}
              onSignal={setSignal}
              onHeart={toggleHeart}
              onCopy={copyLink}
            />
          )}
        </Show>
        <Show when={keysOpen()}>
          <KeyboardMap
            onClose={closeKeys}
            characterShortcuts={characterShortcuts()}
            onCharacterShortcuts={changeCharacterShortcuts}
          />
        </Show>
        <Show when={confirmRemove()}>
          {(item) => (
            <ConfirmRemove
              onCancel={closeConfirmRemove}
              onConfirm={() =>
                completeArchiveRemoval(item, closeConfirmRemove, performHeart)
              }
            />
          )}
        </Show>
        <Show when={finishUndo()} keyed>
          {(operation) => (
            <div
              class="finish-undo-toast"
              role="status"
              aria-live="polite"
              onPointerEnter={() => {
                undoHovered = true;
                syncFinishUndoTimer();
              }}
              onPointerLeave={() => {
                undoHovered = false;
                syncFinishUndoTimer();
              }}
              onFocusIn={() => {
                undoFocused = true;
                syncFinishUndoTimer();
              }}
              onFocusOut={(event) => {
                if (
                  !event.relatedTarget ||
                  !event.currentTarget.contains(event.relatedTarget as Node)
                ) {
                  undoFocused = false;
                  syncFinishUndoTimer();
                }
              }}
            >
              <span>
                <Show
                  when={operation.count > 0}
                  fallback={<strong>Grid cleared</strong>}
                >
                  Marked {operation.count} read <strong>· grid cleared</strong>
                </Show>
              </span>
              <button type="button" onClick={undoLast}>
                Undo
              </button>
              <i class="finish-undo-toast__progress" aria-hidden="true" />
            </div>
          )}
        </Show>
        <Show when={toast()} keyed>
          {(notice) => (
            <div
              class="link-toast"
              classList={{
                error: notice.kind === "error",
                info: notice.kind === "info",
              }}
              role={notice.kind === "error" ? "alert" : "status"}
              aria-live={notice.kind === "error" ? "assertive" : "polite"}
            >
              <Show when={notice.kind !== "info"}>
                <Icon
                  name={notice.kind === "error" ? "close" : "check"}
                  class="toast-icon"
                />
              </Show>
              <span>{notice.message}</span>
            </div>
          )}
        </Show>
      </main>
    </Show>
  );
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

function removeSearchItem(
  response: SearchResponse,
  itemID: string,
): SearchResponse {
  response = normalizeSearchResponse(response);
  const filter = (items: Item[]) =>
    items.filter((item) => item.item_id !== itemID);
  return {
    ...response,
    matches: {
      window: filter(response.matches.window),
      archive: filter(response.matches.archive),
    },
    related: {
      window: filter(response.related.window),
      archive: filter(response.related.archive),
    },
  };
}

function ColdStart(props: { onImport(): void }) {
  return (
    <section class="cold-start">
      <div class="cold-mark">
        <img src="/sema-mark.svg" alt="" aria-hidden="true" />
      </div>
      <h1>Nothing scored yet.</h1>
      <p>
        Sema starts newest-first and sizes items by media and recency. Boost or
        bury a few dozen items and the grid begins shaping itself around what
        you actually read.
      </p>
      <button type="button" onClick={props.onImport}>
        <Icon name="import-opml" />
        Import OPML
      </button>
      <small>ranking activates at ~10 signals · 0 so far</small>
    </section>
  );
}

function ArchiveEmpty() {
  return (
    <section class="archive-empty">
      <Icon name="keep" size={24} filled={false} class="icon-quiet" />
      <h1>Nothing kept yet</h1>
      <p>
        Keep an item and it lands here permanently. Everything else in the feed
        expires after seven days; kept items don&apos;t.
      </p>
    </section>
  );
}

function FilteredEmpty(props: {
  scope: GridScope;
  archive: boolean;
  feedTitle: string;
  onClear(): void;
}) {
  const label = () =>
    props.scope?.kind === "tag" ? `#${props.scope.value}` : props.feedTitle;
  return (
    <section class="archive-empty">
      <h1>No items in {label()}</h1>
      <p>
        {props.archive
          ? "No archived items match this filter."
          : "This filter has no visible items in the current seven-day window."}
      </p>
      <button type="button" class="original-cta" onClick={props.onClear}>
        Show all feeds
      </button>
    </section>
  );
}
