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
import { type AppAPI, UnauthorizedError } from "./api/client";
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
import { UpdateNotice } from "./components/UpdateNotice";
import { feedbackPatch } from "./feedback";
import { createGridModel } from "./grid-model";
import { effectiveGridOrder, sameGridScope } from "./grid-scope";
import { createItemSession } from "./item-session";
import {
  ITEM_WINDOWS,
  type ItemWindow,
  scopeSummary,
  windowRange,
} from "./item-view";
import {
  copyOriginalLink,
  isCancelledShare,
  LinkActionFailure,
} from "./link-action";
import { createMediaQuery } from "./media-query";
import { createReadState } from "./read-state";
import { resolveReaderItem } from "./reader-item";
import { scopeCellModel, windowScopeCounts } from "./scope-cell";
import { normalizeSearchResponse, SEARCH_DEBOUNCE_MS } from "./search";
import { nextSignalNotice, type SignalNotice } from "./signal-feedback";
import { nextThemePreference, type ThemeController } from "./theme";
import type {
  Feed,
  FeedItemCounts,
  GridScope,
  Item,
  Order,
  Profile,
  SearchResponse,
  Story,
} from "./types";
import { ConfirmRemove } from "./ui/ConfirmRemove";
import { Feeds } from "./ui/Feeds";
import { FilterSheet } from "./ui/FilterSheet";
import { frontPageEntriesForState, frontPageSequence } from "./ui/front-page";
import { Grid } from "./ui/Grid";
import { KeyboardMap } from "./ui/KeyboardMap";
import {
  allowsAppShortcut,
  appCommand,
  characterShortcut,
  createGoSequence,
  type GoCommand,
  isEditingTarget,
  readCharacterShortcuts,
  scopeShortcuts,
} from "./ui/keyboard";
import { closeOverlay, keyOwnership, pushOverlay } from "./ui/overlay-history";
import { Reader } from "./ui/Reader";
import { RelatedPanel } from "./ui/RelatedPanel";
import { ScopeBar } from "./ui/ScopeBar";
import { SearchResults } from "./ui/SearchResults";
import { SignalHint } from "./ui/SignalHint";
import { TagFilter } from "./ui/TagFilter";
import { displayFeedTitle, feedScopeChip } from "./ui/tag-options";
import {
  expandToolbar,
  initialToolbarCollapseState,
  scopeChipVisible,
  updateToolbarCollapse,
} from "./ui/toolbar-collapse";
import { createUpdateNotice, type UpdateState } from "./update-notice";
import { listenForWindowReturn } from "./window-activity";

type Toast = {
  id: number;
  kind: "success" | "info" | "error";
  message: string;
};
type HeaderMenu = "overflow";
const READER_EXIT_MS = 220;

export function App(props: {
  signOut(): void;
  theme: ThemeController;
  api: AppAPI;
}) {
  const api = props.api;
  const [, setProfile] = createSignal<Profile>();
  const [signalCount, setSignalCount] = createSignal(5);
  const [signalNotice, setSignalNotice] = createSignal<SignalNotice>();
  let signalNoticeID = 0;
  const [heartCount, setHeartCount] = createSignal(0);
  const [feedFilters, setFeedFilters] = createSignal<Feed[]>([]);
  const [feedItemCounts, setFeedItemCounts] = createSignal<FeedItemCounts>({});
  const [scopeCountsWindowKey, setScopeCountsWindowKey] =
    createSignal<string>();
  const scopePhone = createMediaQuery("(max-width: 619px)");

  const [expandedStoryIDs, setExpandedStoryIDs] = createSignal<Set<string>>(
    new Set(),
  );
  const [error, setError] = createSignal("");
  const [readerID, setReaderID] = createSignal("");
  const [readerClosing, setReaderClosing] = createSignal(false);
  const [readerReveal, setReaderReveal] = createSignal(0);
  const [readerDragging, setReaderDragging] = createSignal(false);
  const [confirmRemove, setConfirmRemove] = createSignal<Item>();
  const [keysOpen, setKeysOpen] = createSignal(false);
  const [characterShortcuts, setCharacterShortcuts] = createSignal(
    readCharacterShortcuts(),
  );
  const [linkActionID, setLinkActionID] = createSignal("");
  const [toast, setToast] = createSignal<Toast>();
  const [view, setView] = createSignal<"grid" | "feeds">("grid");
  const [focusFeedSearch, setFocusFeedSearch] = createSignal(false);
  const [searchQuery, setSearchQuery] = createSignal("");
  const [searchLoading, setSearchLoading] = createSignal(false);
  const [searchFocused, setSearchFocused] = createSignal(false);
  const [searchFocusedID, setSearchFocusedID] = createSignal("");
  const [relatedLoading, setRelatedLoading] = createSignal(false);
  const [readerArchive, setReaderArchive] = createSignal(false);
  const [filterOpen, setFilterOpen] = createSignal(false);
  const [windowCounts, setWindowCounts] = createSignal<
    Partial<Record<ItemWindow, FeedItemCounts>>
  >({});
  const [barHeight, setBarHeight] = createSignal(0);
  const [scopeCollapse, setScopeCollapse] = createSignal(
    initialToolbarCollapseState(),
  );
  const [headerMenu, setHeaderMenu] = createSignal<HeaderMenu>();
  const phoneHeader = createMediaQuery("(max-width: 430px)");
  const compactDisplayControls = createMediaQuery("(max-width: 859px)");
  const [tagFilterOpen, setTagFilterOpen] = createSignal(false);
  const [tagOpenRequest, setTagOpenRequest] = createSignal(0);
  let feedItemCountVersion = 0;
  let searchVersion = 0;
  let relatedVersion = 0;
  let readerCloseTimer: number | undefined;
  const goSequence = createGoSequence();
  let gridHome: (() => void) | undefined;
  const keyboard = () =>
    keyOwnership(
      Boolean(headerMenu()) || tagFilterOpen(),
      readerID() && view() === "grid" ? "reader" : "grid",
    );
  let feedsGridDirty = false;
  let feedFilterRefresh: Promise<void> | undefined;
  let linkActionTimer: number | undefined;
  let toastTimer: number | undefined;
  let toastID = 0;
  let searchInput!: HTMLInputElement;
  const pendingEvents = new Map<string, BehaviourEvent>();
  const heartsInFlight = new Set<string>();
  const session = createItemSession(api, {
    onError: (error) => handleError(error),
    changed: () => {
      void refreshFeedItemCounts();
    },
    beforeReload: () => {
      discardFinishUndo();
      setExpandedStoryIDs(new Set<string>());
    },
    pagingBlocked: () =>
      Boolean(readerID() || view() !== "grid" || searchActive()),
    cleared: () => finishUndo(),
  });
  const {
    items,
    setItems,
    stories,
    readAnchor,
    gridIDs,
    setGridIDs,
    gridStoryIDs,
    pendingNew,
    layoutVersion,
    setLayoutVersion,
    scrollTopVersion,
    setScrollTopVersion,
    scrollTarget,
    setScrollTarget,
    cursor,
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
  } = session;
  let eventTimer: number | undefined;
  const readState = createReadState(api, session, (error) =>
    handleError(error),
  );
  const {
    flushRead,
    recordOpened,
    toggleRead,
    toggleStoryRead,
    undoLast,
    finishAndClear,
    discardFinishUndo,
    readAdjust,
    setReadAdjust,
    undo,
  } = readState;
  const markBelow = (item: Item) => readState.markBelow(item.item_id);
  const markStoryBelow = (id: string) => readState.markBelow(`story:${id}`);
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
      itemWindow(),
      unreadOnly(),
      feedFilters(),
      scopeCountsWindowKey() === (windowRange(itemWindow())?.from ?? "")
        ? feedItemCounts()
        : undefined,
      readAdjust(),
      scopePhone(),
    );
    return model;
  });

  const visibleScopeCell = createMemo(() =>
    !scope() &&
    unreadOnly() &&
    itemWindow() === "all" &&
    scopeCell()?.count === 0
      ? undefined
      : scopeCell(),
  );
  const showScopeChip = createMemo(() =>
    scopeChipVisible(
      scopeCollapse().lastScrollTop,
      barHeight(),
      scopeCollapse(),
    ),
  );
  const pendingWindowCounts = new Map<ItemWindow, number>();
  createEffect(() => {
    if (!filterOpen()) return;
    const cached = windowCounts();
    const version = feedItemCountVersion;
    for (const { value } of ITEM_WINDOWS) {
      if (
        value === itemWindow() ||
        cached[value] ||
        pendingWindowCounts.get(value) === version
      )
        continue;
      pendingWindowCounts.set(value, version);
      void api
        .feedItemCounts(windowRange(value))
        .then((counts) => {
          if (version === feedItemCountVersion && counts)
            setWindowCounts((current) => ({ ...current, [value]: counts }));
        })
        .catch(handleError)
        .finally(() => {
          if (pendingWindowCounts.get(value) === version)
            pendingWindowCounts.delete(value);
        });
    }
  });
  createEffect(() => {
    if (filterOpen()) {
      pushOverlay("filter-sheet", () => setFilterOpen(false), false);
      onCleanup(() => closeOverlay("filter-sheet"));
    }
  });
  const openFilter = () => {
    setScopeCollapse(expandToolbar);
    setFilterOpen(true);
  };

  const bootstrap = async () => {
    setLoading(true);
    setError("");
    try {
      const me = await api.me();
      setProfile(me.profile);
      setSignalCount(me.signal_count);
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

  createEffect(() => {
    scope();
    view();
    itemWindow();
    mode();
    gridOrder();
    unreadOnly();
    resetPoll();
  });

  let previousSearchScope: GridScope = null;
  createEffect(() => {
    const query = searchQuery().trim();
    const searchScope = scope();
    const scopeChanged =
      searchScope?.kind !== previousSearchScope?.kind ||
      searchScope?.value !== previousSearchScope?.value;
    previousSearchScope = searchScope;
    const version = ++searchVersion;
    if ([...query].length < 2) {
      setSearchResponse();
      setSearchFocusedID("");
      setSearchLoading(false);
      return;
    }
    if (scopeChanged) {
      setSearchResponse();
      setSearchFocusedID("");
    }
    setSearchLoading(true);
    const timer = window.setTimeout(() => {
      api
        .search(query, searchScope)
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

  const clearGo = () => goSequence.clear();
  const goSequenceAvailable = () => allowsAppShortcut("go-prefix", keyboard());

  const announcedUpdateBuilds = new Set<string>();
  const [updateState, setUpdateState] = createSignal<UpdateState>();
  let updateNotice: ReturnType<typeof createUpdateNotice> | undefined;
  onMount(() => {
    updateNotice = createUpdateNotice({
      currentBuild: __SEMA_BUILD__,
      fetch: (url, init) => fetch(url, init),
      storage: {
        getItem: (key) => localStorage.getItem(key),
        setItem: (key, value) => localStorage.setItem(key, value),
      },
      now: Date.now,
      visible: () => document.visibilityState === "visible",
      setInterval: (callback, ms) => window.setInterval(callback, ms),
      clearInterval: (timer) => window.clearInterval(timer as number),
      changed: setUpdateState,
      flush: () => flushPending(true),
      reload: () => location.reload(),
    });
    bootstrap();
    const flush = () => void flushPending(true);
    const stopWindowReturn = listenForWindowReturn(flush, () => {
      void pollNew();
      void updateNotice?.onReturn();
    });
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
      if (!goSequenceAvailable()) {
        clearGo();
        return;
      }
      const command = goSequence.key(event.key, performance.now());
      if (command) {
        event.preventDefault();
        if (command === "home") gridHome?.();
        else if (command !== "prefix") void navigateByKey(command);
      }
    };
    const clearGoOnFocus = () => clearGo();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.isComposing) return;
      const editing = isEditingTarget(event.target);
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
        allowsAppShortcut("undo", keyboard())
      ) {
        event.preventDefault();
        undoLast();
        return;
      }
      if (
        event.key === "/" &&
        allowsAppShortcut("search", keyboard()) &&
        !editing
      ) {
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
      if (event.key === "Escape" && keyboard().owner === "feeds") {
        event.preventDefault();
        void closeFeedsAndSettings();
        return;
      }
      const command = appCommand(event.key);
      if (
        !command ||
        !allowsAppShortcut(command, keyboard()) ||
        (command === "close-help" && !keysOpen())
      )
        return;
      if (
        command === "toggle-unread" &&
        (view() !== "grid" || mode() !== "live" || loading())
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
    const onPollVisibility = () => {
      if (document.visibilityState === "visible") resetPoll();
    };
    document.addEventListener("visibilitychange", onPollVisibility);
    resetPoll();
    window.addEventListener("keydown", onShortcutCapture, true);
    window.addEventListener("blur", clearGoOnFocus);
    window.addEventListener("focusin", clearGoOnFocus);
    window.addEventListener("pagehide", flush);
    window.addEventListener("keydown", onKeyDown);
    onCleanup(() => {
      window.removeEventListener("keydown", onShortcutCapture, true);
      window.removeEventListener("blur", clearGoOnFocus);
      window.removeEventListener("focusin", clearGoOnFocus);
      stopWindowReturn();
      updateNotice?.dispose();
      window.removeEventListener("pagehide", flush);
      window.removeEventListener("keydown", onKeyDown);
      window.clearTimeout(eventTimer);
      window.clearTimeout(readerCloseTimer);
      document.removeEventListener("visibilitychange", onPollVisibility);
      session.dispose();
      window.clearTimeout(linkActionTimer);
      window.clearTimeout(toastTimer);
      readState.dispose();
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

  createEffect(() => {
    // Navigation retires desktop feedback; reader actions share the lifetime gate.
    view();
    mode();
    scope();
    readerID();
    searchActive();
    itemWindow();
    unreadOnly();
    order();
    setSignalNotice(undefined);
  });

  const setSignal = (item: Item, value: -1 | 0 | 1) => {
    const patch = feedbackPatch(item, "signal", value, mode() === "archive");
    if (!patch) return;
    const previous = item.signal;
    const effective = patch.signal;
    const noticeID = ++signalNoticeID;
    setSignalNotice(
      nextSignalNotice(signalCount(), noticeID, item.item_id, effective),
    );
    setSignalCount((count) => count + 1);
    replaceItem(item.item_id, { signal: effective });
    api.signal(item.item_id, value).catch((caught) => {
      replaceItem(item.item_id, { signal: previous });
      setSignalNotice((notice) =>
        notice?.id === noticeID ? undefined : notice,
      );
      handleError(caught);
    });
  };

  const performHeart = async (item: Item) => {
    if (heartsInFlight.has(item.item_id)) return;
    heartsInFlight.add(item.item_id);
    const previous = item.hearted;
    const next = !previous;
    replaceItem(
      item.item_id,
      feedbackPatch(item, next ? "keep" : "unkeep") ?? {},
    );
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
      replaceItem(item.item_id, { hearted: previous, signal: item.signal });
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

  const queueEvent = (itemID: string, event: BehaviourEvent) => {
    const current = pendingEvents.get(itemID) ?? {};
    pendingEvents.set(itemID, mergeBehaviourEvent(current, event));
    window.clearTimeout(eventTimer);
    eventTimer = window.setTimeout(() => void flushPending(), 5_000);
  };

  const flushEvents = (keepalive = false) => {
    const events = [...pendingEvents.entries()];
    pendingEvents.clear();
    return Promise.all(
      events.map(([itemID, event]) => api.behaviour(itemID, event, keepalive)),
    ).catch(handleError);
  };

  const flushPending = (keepalive = false) => {
    window.clearTimeout(eventTimer);
    eventTimer = undefined;
    return Promise.all([flushRead(keepalive), flushEvents(keepalive)]);
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
    openReaderHistory();
    setReaderItem({ ...lead, read: true });
    setReaderArchive(false);
    setReaderID(lead.item_id);
    readState.openStory(story);
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

  const selectWindow = async (next: ItemWindow) => {
    if (mode() === "archive" || next === itemWindow()) return;
    await flushRead();
    setItemWindow(next);
    void reload();
  };

  const setUnreadOnlyState = async (next: boolean) => {
    if (mode() === "archive" || next === unreadOnly()) return;
    await flushRead();
    setUnreadOnly(next);
    void reload();
  };
  const toggleUnread = () => setUnreadOnlyState(!unreadOnly());

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
    setWindowCounts({});
    const window = view() === "feeds" ? undefined : windowRange(itemWindow());
    if (scopeCountsWindowKey() !== (window?.from ?? ""))
      setScopeCountsWindowKey(undefined);
    try {
      // Counts must include queued and already-in-flight read writes before
      // they replace the optimistic adjustment (including finish-and-clear).
      await flushRead();
      if (!(await readState.settle()) || version !== feedItemCountVersion)
        return;
      const readRevision = readState.revision;
      const latest = await api.feedItemCounts(window);
      if (!latest) return;
      if (
        version === feedItemCountVersion &&
        readRevision === readState.revision
      )
        batch(() => {
          setFeedItemCounts(latest ?? {});
          setWindowCounts((current) => ({
            ...current,
            [view() === "feeds" ? "all" : itemWindow()]: latest,
          }));
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

  const openFeedsAndSettings = (focusSearch = false) => {
    setFocusFeedSearch(focusSearch);
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
    if (scopeCountsWindowKey() !== (windowRange(itemWindow())?.from ?? ""))
      void refreshFeedItemCounts();
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
    if (view() === "feeds") closeOverlay("feeds");
    setView("grid");
    closeReader();
    closeConfirmRemove();
    await reload(order(), unreadOnly(), next);
  };

  const navigateByKey = async (command: GoCommand) => {
    if (command === "settings") {
      if (view() === "feeds") await closeFeedsAndSettings();
      else openFeedsAndSettings(true);
      return;
    }
    if (view() === "feeds") await closeFeedsAndSettings();
    if (command === "archive") {
      if (mode() !== "archive") await toggleArchive();
      return;
    }
    if (mode() === "archive") {
      await flushRead();
      if (command === "unread") setUnreadOnly(!unreadOnly());
      else setItemWindow(command);
      setMode("live");
      await reload();
    } else {
      if (command === "unread") await toggleUnread();
      else await selectWindow(command);
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

  const gridModel = createMemo(() =>
    createGridModel({
      scopeCell: !compactDisplayControls() ? visibleScopeCell() : undefined,
      scope: scope(),
      scopeTitle: scopeCell()?.title,
      itemWindow: itemWindow(),
      clearedCount: finishUndo()?.count,
      items: gridItems(),
      entries: frontPageEntries(),
      stories: gridStories(),
      expandedStoryIDs: expandedStoryIDs(),
      hasMore: cursor() !== "",
      archive: mode() === "archive",
      unreadOnly: unreadOnly(),
      order: gridOrder(),
    }),
  );

  return (
    <Show
      when={view() === "grid"}
      fallback={
        <>
          <Feeds
            api={api}
            itemCounts={
              scopeCountsWindowKey() === "" ? feedItemCounts() : undefined
            }
            onRefreshCounts={refreshFeedItemCounts}
            focusSearch={focusFeedSearch()}
            heartCount={heartCount()}
            onBack={() => void closeFeedsAndSettings()}
            onKeys={openKeys}
            characterShortcuts={characterShortcuts()}
            onCharacterShortcuts={changeCharacterShortcuts}
            onSignOut={props.signOut}
            onFeedsChanged={noteFeedsChanged}
            onToast={showToast}
          />
          <ToastNotice notice={toast()} />
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
            <span class="sr-only" aria-live="polite">
              {scopeSummary(itemWindow(), unreadOnly())}
            </span>
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
                  <For each={ITEM_WINDOWS}>
                    {(option) => (
                      <button
                        type="button"
                        class="segmented__item"
                        classList={{ active: itemWindow() === option.value }}
                        role="radio"
                        aria-checked={itemWindow() === option.value}
                        title={`${option.label} (${scopeShortcuts[option.value]})`}
                        onClick={() => void selectWindow(option.value)}
                      >
                        <span>{option.label}</span>
                      </button>
                    )}
                  </For>
                </div>
                <button
                  type="button"
                  class="chrome-btn"
                  classList={{ "chrome-btn--on": unreadOnly() }}
                  role="switch"
                  aria-checked={unreadOnly()}
                  onClick={() => void toggleUnread()}
                >
                  Unread
                </button>
              </div>
              <Show when={compactDisplayControls() && !searchActive()}>
                <button
                  type="button"
                  class="chrome-btn scope-header-chip"
                  classList={{ "scope-header-chip--visible": showScopeChip() }}
                  aria-hidden={!showScopeChip()}
                  tabIndex={showScopeChip() ? 0 : -1}
                  aria-haspopup="dialog"
                  onClick={openFilter}
                >
                  {scopeSummary(itemWindow(), unreadOnly())}
                  <Icon name="chevron-down" size={13} />
                </button>
              </Show>
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
                !headerMenu() &&
                ["grid", "search", "transient"].includes(keyboard().owner)
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
              onClick={() => openFeedsAndSettings()}
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
        <SignalHint
          notice={signalNotice()}
          visible={
            view() === "grid" &&
            mode() !== "archive" &&
            !readerID() &&
            !searchActive()
          }
          onUndo={(notice) => {
            const item =
              items().find(
                (candidate) => candidate.item_id === notice.itemID,
              ) ??
              stories()
                .flatMap((story) => story.items)
                .find((candidate) => candidate.item_id === notice.itemID);
            if (item) setSignal(item, 0);
          }}
        />
        <Show
          when={updateState()?.available && !readerID() && !readerClosing()}
        >
          <UpdateNotice
            state={updateState() as UpdateState}
            announcedBuilds={announcedUpdateBuilds}
            onReload={() => void updateNotice?.reload()}
            onDismiss={() => updateNotice?.dismiss()}
          />
        </Show>
        <Show when={filterOpen()}>
          <FilterSheet
            window={itemWindow()}
            unreadOnly={unreadOnly()}
            counts={{
              ...windowScopeCounts(
                scope(),
                unreadOnly(),
                feedFilters(),
                windowCounts(),
              ),
              [itemWindow()]: scopeCell()?.count,
            }}
            onWindow={(next) => void selectWindow(next)}
            onUnreadOnly={(next) => void setUnreadOnlyState(next)}
            onClose={() => setFilterOpen(false)}
          />
        </Show>
        <Show when={headerMenu() === "overflow"}>
          <div class="overflow-sheet-layer">
            <button
              type="button"
              class="overflow-sheet-backdrop"
              aria-label="Close more options"
              onClick={() => setHeaderMenu()}
            />
            <section
              class="overflow-sheet header-overflow-sheet"
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
              <button type="button" onClick={() => openFeedsAndSettings()}>
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
            classList={{ "new-items-pill--scope": Boolean(visibleScopeCell()) }}
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
                  !unreadOnly() ||
                  itemWindow() !== "all"))
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
              model={gridModel()}
              pendingNewCount={pendingNew().length}
              layout={{
                scrollTarget: scrollTarget(),
                focusedID: focusedID(),
                scrollToTopKey: scrollTopVersion(),
                initialScrollTop: session.scrollTop,
                layoutKey: layoutVersion(),
              }}
              reader={{
                readerReveal: readerReveal(),
                readerOpen: Boolean(readerID()),
                readerDragging: readerDragging(),
              }}
              actions={{
                onClearScope: () =>
                  scope()?.kind === "feed"
                    ? void applyFeed("")
                    : void applyTag(""),
                onShowAll: () => void selectWindow("all"),
                onShowRead: () => void setUnreadOnlyState(false),
                onOpenArchive: () => void navigateByKey("archive"),
                onSelectView: (view) => void selectWindow(view),
                onFocus: setFocusedID,
                onOpen: markOpened,
                onOpenStoryLead: markStoryOpened,
                onExternalOpen: openExternalItem,
                onDiscussion: recordClickThrough,
                onSignal: setSignal,
                onHeart: toggleHeart,
                onToggleRead: toggleRead,
                onToggleStoryRead: toggleStoryRead,
                onCopy: copyLink,
                onOriginal: openOriginal,
                onRelated: openRelated,
                onApplyFeed: (item) => void applyFeed(item.feed_id),
                onMarkBelow: markBelow,
                onMarkStoryBelow: markStoryBelow,
                onExpandStory: (storyID) =>
                  setExpandedStoryIDs((current) => {
                    const next = new Set(current);
                    if (next.has(storyID)) next.delete(storyID);
                    else next.add(storyID);
                    return next;
                  }),
                onToggleOrder: toggleOrder,
                onUndo: undoLast,
              }}
              onPassed={readState.onPassed}
              onFinishAndClear={() => finishAndClear()}
              onReachedEnd={loadMore}
              active={keyboard().owner === "grid"}
              onHomeReady={(action) => {
                gridHome = action;
              }}
              topSlot={
                compactDisplayControls() &&
                mode() === "live" &&
                !searchActive() ? (
                  <ScopeBar
                    scope={scope()}
                    model={scopeCell()}
                    window={itemWindow()}
                    unreadOnly={unreadOnly()}
                    order={gridOrder()}
                    onClearScope={() => void applyScope(null)}
                    onOrder={(next) => void selectOrder(next)}
                    onFilter={openFilter}
                  />
                ) : undefined
              }
              onTopSlotHeight={setBarHeight}
              onRefresh={() => pollNew(true)}
              onScrollPosition={(top) => {
                session.scrollTop = top;
                setScopeCollapse((current) =>
                  updateToolbarCollapse(current, top, false),
                );
              }}
            />
          </Show>
        </Show>
        <Show when={loadingMore()}>
          <div class="page-loader">fetching more…</div>
        </Show>
        <Show when={searchActive()}>
          <SearchResults
            scopeLabel={
              scope()?.kind === "tag"
                ? `#${scope()?.value}`
                : feedScopeChip(scope(), feedFilters())?.title
            }
            query={searchQuery().trim()}
            response={searchResponse()}
            loading={searchLoading()}
            focusedID={searchFocusedID()}
            active={keyboard().owner === "search"}
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
              loadBody={(url, signal) => api.body(url, signal)}
              item={item()}
              active={
                keyboard().owner === "reader" ||
                (keyboard().owner === "action-sheet" &&
                  keyboard().overlays.includes("reader"))
              }
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
              active={keyboard().owner === "related"}
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
                readState.pauseUndo("hover", true);
              }}
              onPointerLeave={() => {
                readState.pauseUndo("hover", false);
              }}
              onFocusIn={() => {
                readState.pauseUndo("focus", true);
              }}
              onFocusOut={(event) => {
                if (
                  !event.relatedTarget ||
                  !event.currentTarget.contains(event.relatedTarget as Node)
                ) {
                  readState.pauseUndo("focus", false);
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
        <ToastNotice notice={toast()} />
      </main>
    </Show>
  );
}

function ToastNotice(props: { notice?: Toast }) {
  return (
    <Show when={props.notice} keyed>
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
  );
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
