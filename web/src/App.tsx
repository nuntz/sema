import {
  createEffect,
  createMemo,
  createSignal,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import { APIClient, UnauthorizedError } from "./api/client";
import { shouldConfirmArchiveRemoval } from "./archive";
import {
  type BehaviourEvent,
  linkBehaviourEvent,
  mergeBehaviourEvent,
} from "./behaviour-events";
import { Icon } from "./components/Icon";
import {
  mergeNewItems,
  pollCandidates,
  unreadIDsAfter,
  updateRead,
  visibleItemIDs,
} from "./item-list";
import {
  copyOriginalLink,
  isCancelledShare,
  LinkActionFailure,
} from "./link-action";
import { normalizeSearchResponse, SEARCH_DEBOUNCE_MS } from "./search";
import type { Feed, Item, Order, Profile, SearchResponse } from "./types";
import { ConfirmRemove } from "./ui/ConfirmRemove";
import { Feeds } from "./ui/Feeds";
import { Grid } from "./ui/Grid";
import { KeyboardMap } from "./ui/KeyboardMap";
import { appCommand } from "./ui/keyboard";
import { Reader } from "./ui/Reader";
import { RelatedPanel } from "./ui/RelatedPanel";
import { SearchResults } from "./ui/SearchResults";
import { TagFilter } from "./ui/TagFilter";

type Undo = { ids: string[] };
type Toast = { id: number; kind: "success" | "error"; message: string };

export function App(props: { token: () => string; signOut(): void }) {
  const api = new APIClient(props.token);
  const [profile, setProfile] = createSignal<Profile>();
  const [signalCount, setSignalCount] = createSignal(0);
  const [heartCount, setHeartCount] = createSignal(0);
  const [order, setOrder] = createSignal<Order>("chrono");
  const [selectedTag, setSelectedTag] = createSignal("");
  const [feedFilters, setFeedFilters] = createSignal<Feed[]>([]);
  const [items, setItems] = createSignal<Item[]>([]);
  const [gridIDs, setGridIDs] = createSignal<string[]>([]);
  const [pendingNew, setPendingNew] = createSignal<Item[]>([]);
  const [layoutVersion, setLayoutVersion] = createSignal(0);
  const [scrollTopVersion, setScrollTopVersion] = createSignal(0);
  const [cursor, setCursor] = createSignal("");
  const [hasPage, setHasPage] = createSignal(false);
  const [loading, setLoading] = createSignal(true);
  const [loadingMore, setLoadingMore] = createSignal(false);
  const [error, setError] = createSignal("");
  const [unreadOnly, setUnreadOnly] = createSignal(true);
  const [focusedID, setFocusedID] = createSignal("");
  const [readerID, setReaderID] = createSignal("");
  const [mode, setMode] = createSignal<"live" | "archive">("live");
  const [confirmRemove, setConfirmRemove] = createSignal<Item>();
  const [keysOpen, setKeysOpen] = createSignal(false);
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
  let requestVersion = 0;
  let searchVersion = 0;
  let relatedVersion = 0;
  let readTimer: number | undefined;
  let pollTimer: number | undefined;
  let pollInFlight = false;
  let markBelowInFlight = false;
  let linkActionTimer: number | undefined;
  let toastTimer: number | undefined;
  let toastID = 0;
  let searchInput!: HTMLInputElement;
  const pendingRead = new Set<string>();
  const pendingEvents = new Map<string, BehaviourEvent>();
  const heartsInFlight = new Set<string>();
  const searchActive = createMemo(() => [...searchQuery().trim()].length >= 2);

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

  const bootstrap = async () => {
    setLoading(true);
    setError("");
    try {
      const me = await api.me();
      setProfile(me.profile);
      setSignalCount(me.signal_count);
      setHeartCount(me.heart_count ?? me.profile.heart_count ?? 0);
      setOrder(me.profile.order_pref || "chrono");
      const profileTag = me.profile.tag_pref || "";
      setSelectedTag(profileTag);
      const [availableFeeds] = await Promise.all([
        api.feeds(),
        reload(
          me.profile.order_pref || "chrono",
          unreadOnly(),
          "live",
          profileTag,
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
    nextTag = selectedTag(),
  ) => {
    const version = ++requestVersion;
    setLoading(true);
    setHasPage(false);
    setItems([]);
    setGridIDs([]);
    setPendingNew([]);
    setCursor("");
    try {
      const page =
        nextMode === "archive"
          ? await api.archive()
          : await api.items(nextOrder, "", !nextUnreadOnly, nextTag);
      if (version !== requestVersion) return;
      const pageItems = page.items ?? [];
      setItems(pageItems);
      const visibleIDs =
        nextMode === "archive"
          ? pageItems.map((item) => item.item_id)
          : visibleItemIDs(pageItems, nextUnreadOnly);
      setGridIDs(visibleIDs);
      setLayoutVersion((value) => value + 1);
      setScrollTopVersion((value) => value + 1);
      setCursor(page.next_cursor ?? "");
      setHasPage(true);
      setFocusedID(visibleIDs[0] ?? "");
    } catch (caught) {
      handleError(caught);
    } finally {
      if (version === requestVersion) setLoading(false);
    }
  };

  const loadMore = async () => {
    if (loadingMore() || !hasPage() || !cursor()) return;
    setLoadingMore(true);
    const nextCursor = cursor();
    let continueLoading = false;
    try {
      const page =
        mode() === "archive"
          ? await api.archive(nextCursor)
          : await api.items(order(), nextCursor, !unreadOnly(), selectedTag());
      if (nextCursor !== cursor()) return;
      let added: Item[] = [];
      setItems((current) => {
        const seen = new Set(current.map((item) => item.item_id));
        added = (page.items ?? []).filter((item) => !seen.has(item.item_id));
        return added.length > 0 ? [...current, ...added] : current;
      });
      const visible =
        mode() === "archive"
          ? added.map((item) => item.item_id)
          : visibleItemIDs(added, unreadOnly());
      if (visible.length > 0) {
        setGridIDs((current) => [...current, ...visible]);
        setLayoutVersion((value) => value + 1);
      }
      setCursor(page.next_cursor ?? "");
      continueLoading = visible.length === 0 && (page.next_cursor ?? "") !== "";
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
    const version = requestVersion;
    pollInFlight = true;
    try {
      const page = await api.items("chrono", "", false, selectedTag());
      if (version !== requestVersion) return 0;
      const unseen = pollCandidates(
        items(),
        pendingNew(),
        page.items ?? [],
        unreadOnly(),
      );
      if (insert) {
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

  onMount(() => {
    bootstrap();
    const flush = () => void flushPending(true);
    const onVisibility = () => {
      if (document.visibilityState === "hidden") flush();
      else void pollNew();
    };
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target;
      if (
        event.key === "/" &&
        !readerID() &&
        !relatedSource() &&
        !(
          target instanceof HTMLElement &&
          (target.isContentEditable ||
            target.matches("input, textarea, select"))
        )
      ) {
        event.preventDefault();
        searchInput.focus();
        searchInput.select();
        return;
      }
      const command = appCommand(event.key);
      if (
        event.repeat ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey ||
        !command ||
        (command === "close-help" && !keysOpen())
      )
        return;
      if (
        target instanceof HTMLElement &&
        (target.isContentEditable || target.matches("input, textarea, select"))
      )
        return;
      event.preventDefault();
      if (command === "toggle-archive") {
        setKeysOpen(false);
        void toggleArchive();
      } else {
        setKeysOpen((open) => (command === "toggle-help" ? !open : false));
      }
    };
    pollTimer = window.setInterval(() => void pollNew(), 60_000);
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("pagehide", flush);
    window.addEventListener("keydown", onKeyDown);
    onCleanup(() => {
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("pagehide", flush);
      window.removeEventListener("keydown", onKeyDown);
      window.clearTimeout(readTimer);
      window.clearInterval(pollTimer);
      window.clearTimeout(linkActionTimer);
      window.clearTimeout(toastTimer);
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
    return (
      items().find((item) => item.item_id === id) ??
      searchItems().find((item) => item.item_id === id) ??
      relatedItems().find((item) => item.item_id === id)
    );
  });
  const selectedIndex = createMemo(() =>
    gridItems().findIndex((item) => item.item_id === readerID()),
  );

  const replaceItem = (itemID: string, patch: Partial<Item>) => {
    setItems((current) =>
      current.map((item) =>
        item.item_id === itemID ? { ...item, ...patch } : item,
      ),
    );
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
  };

  const openRelated = (item: Item) => {
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
    relatedVersion++;
    setRelatedSource();
    setRelatedItems([]);
    setRelatedLoading(false);
  };

  const setSignal = (item: Item, value: -1 | 0 | 1) => {
    const previous = item.signal;
    const effective = item.hearted && value === 0 ? 1 : value;
    replaceItem(item.item_id, { signal: effective });
    api
      .signal(item.item_id, value)
      .then(() => {
        if (previous === 0 && effective !== 0)
          setSignalCount((count) => count + 1);
        if (previous !== 0 && effective === 0)
          setSignalCount((count) => Math.max(0, count - 1));
      })
      .catch((caught) => {
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
        setReaderID((current) => (current === item.item_id ? "" : current));
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

  const queueRead = (ids: string[]) => {
    if (mode() === "archive") return;
    const requested = new Set(ids);
    const alreadyRead = new Set(
      items()
        .filter((item) => item.read)
        .map((item) => item.item_id),
    );
    const unread = [...requested].filter((id) => !alreadyRead.has(id));
    if (unread.length === 0) return;
    for (const id of unread) pendingRead.add(id);
    setUndo({ ids: [...pendingRead] });
    setItems((current) => updateRead(current, requested, true));
    if (document.visibilityState === "hidden") {
      void flushRead(true);
      return;
    }
    window.clearTimeout(readTimer);
    readTimer = window.setTimeout(() => void flushPending(), 5_000);
  };

  const flushRead = (keepalive = false) => {
    window.clearTimeout(readTimer);
    readTimer = undefined;
    const ids = [...pendingRead];
    if (ids.length === 0) return Promise.resolve();
    pendingRead.clear();
    setUndo({ ids });
    return writeReadBatch(ids, true, keepalive).catch(handleError);
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

  const markOpened = (item: Item, archive = item.archived === true) => {
    if (!archive) api.events(item.item_id, { opened: true }).catch(handleError);
    if (!archive && !item.read) {
      replaceItem(item.item_id, { read: true });
      api.read(item.item_id, true).catch((caught) => {
        replaceItem(item.item_id, { read: false });
        handleError(caught);
      });
    }
    setReaderArchive(archive);
    setReaderID(item.item_id);
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
    replaceItem(item.item_id, { read: !item.read });
    api.read(item.item_id, !item.read).catch((caught) => {
      replaceItem(item.item_id, { read: item.read });
      handleError(caught);
    });
  };

  const undoLast = () => {
    const operation = undo();
    if (!operation) return;
    setUndo(undefined);
    const unsent = new Set(
      operation.ids.filter((id) => pendingRead.delete(id)),
    );
    if (pendingRead.size === 0) {
      window.clearTimeout(readTimer);
      readTimer = undefined;
    }
    setItems((current) => updateRead(current, operation.ids, false));
    const sent = operation.ids.filter((id) => !unsent.has(id));
    if (sent.length > 0) writeReadBatch(sent, false).catch(handleError);
  };

  const markBelow = async (item: Item) => {
    if (mode() === "archive" || markBelowInFlight) return;
    markBelowInFlight = true;
    const version = requestVersion;
    const markOrder = order();
    const ids = unreadIDsAfter(items(), item.item_id);
    let nextCursor = cursor();
    try {
      while (nextCursor !== "") {
        const page = await api.items(
          markOrder,
          nextCursor,
          !unreadOnly(),
          selectedTag(),
        );
        if (version !== requestVersion || markOrder !== order()) return;
        ids.push(
          ...(page.items ?? [])
            .filter((candidate) => !candidate.read)
            .map((candidate) => candidate.item_id),
        );
        nextCursor = page.next_cursor ?? "";
      }
      queueRead(ids);
    } catch (caught) {
      handleError(caught);
    } finally {
      markBelowInFlight = false;
    }
  };

  const toggleUnread = async () => {
    if (mode() === "archive") return;
    await flushRead();
    const next = !unreadOnly();
    setUnreadOnly(next);
    void reload(order(), next);
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
      const addedIDs = new Set(visible);
      setGridIDs((current) => [
        ...visible,
        ...current.filter((id) => !addedIDs.has(id)),
      ]);
      setFocusedID(visible[0]);
      setLayoutVersion((value) => value + 1);
    }
    setScrollTopVersion((value) => value + 1);
    return added.length;
  };

  const insertPendingNew = () => insertNewItems(pendingNew());

  const toggleOrder = async () => {
    if (mode() === "archive") return;
    await flushRead();
    const next: Order = order() === "chrono" ? "interest" : "chrono";
    setOrder(next);
    setReaderID("");
    setProfile((current) =>
      current ? { ...current, order_pref: next } : current,
    );
    api.patchMe({ order_pref: next }).catch(handleError);
    await reload(next);
  };

  const applyTag = async (tag: string) => {
    if (mode() === "archive" || tag === selectedTag()) return;
    await flushRead();
    setSelectedTag(tag);
    setReaderID("");
    setProfile((current) =>
      current ? { ...current, tag_pref: tag } : current,
    );
    api.patchMe({ tag_pref: tag }).catch(handleError);
    await reload(order(), unreadOnly(), "live", tag);
  };

  const refreshFeedFilters = async () => {
    try {
      const latest = await api.feeds();
      setFeedFilters(latest);
      const activeTag = selectedTag();
      if (
        activeTag &&
        activeTag !== "untagged" &&
        !latest.some((feed) => feed.tags?.includes(activeTag))
      ) {
        setSelectedTag("");
        setProfile((current) =>
          current ? { ...current, tag_pref: "" } : current,
        );
        api.patchMe({ tag_pref: "" }).catch(handleError);
      }
    } catch (caught) {
      handleError(caught);
    }
  };

  const returnFromFeeds = async () => {
    setView("grid");
    await refreshFeedFilters();
    await reload();
  };

  const toggleArchive = async () => {
    await flushRead();
    const next = mode() === "live" ? "archive" : "live";
    setMode(next);
    setView("grid");
    setReaderID("");
    setConfirmRemove();
    await reload(order(), unreadOnly(), next);
  };

  const moveReader = (delta: number) => {
    const next = gridItems()[selectedIndex() + delta];
    if (next) markOpened(next);
  };

  const initials = () =>
    (profile()?.email || "SE")
      .split(/[@._-]/)
      .slice(0, 2)
      .map((part) => part[0]?.toUpperCase())
      .join("");

  return (
    <Show
      when={view() === "grid"}
      fallback={
        <>
          <Feeds
            api={api}
            heartCount={heartCount()}
            onBack={() => void returnFromFeeds()}
            onKeys={() => setKeysOpen(true)}
            onSignOut={props.signOut}
            onFeedsChanged={() => void refreshFeedFilters()}
            onToast={showToast}
          />
          <Show when={keysOpen()}>
            <KeyboardMap onClose={() => setKeysOpen(false)} />
          </Show>
        </>
      }
    >
      <main
        class="app-shell"
        classList={{
          searching: searchActive(),
          "search-focused": searchFocused(),
        }}
      >
        <header class="topbar">
          <button
            type="button"
            class="wordmark"
            classList={{ quiet: mode() === "archive" }}
            onClick={() => setView("grid")}
          >
            Sema
          </button>
          <Show
            when={mode() === "live"}
            fallback={
              <span class="archive-eyebrow">ARCHIVE · {heartCount()} KEPT</span>
            }
          >
            <fieldset class="order-toggle" aria-label="Item order">
              <button
                type="button"
                classList={{ active: order() === "chrono" }}
                onClick={() => order() !== "chrono" && toggleOrder()}
              >
                CHRONO
              </button>
              <button
                type="button"
                classList={{ active: order() === "interest" }}
                onClick={() => order() !== "interest" && toggleOrder()}
              >
                INTEREST
              </button>
            </fieldset>
            <span class="tag-filter-divider" aria-hidden="true" />
            <TagFilter
              feeds={feedFilters()}
              value={selectedTag()}
              active={!readerID() && !keysOpen() && !confirmRemove()}
              onChange={(tag) => void applyTag(tag)}
            />
            <span class="status-line">
              {items().filter((item) => !item.read).length} unread ·{" "}
              {order() === "interest"
                ? `ranked from ${signalCount()} signals`
                : `${items().length} recent items`}
            </span>
            <span class="mobile-status">
              {items().filter((item) => !item.read).length} unread
            </span>
          </Show>
          <div class="search-slot">
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
                placeholder={
                  searchFocused() ? "Search or describe a topic" : "Search"
                }
                aria-label="Search window and archive"
                onFocus={() => setSearchFocused(true)}
                onBlur={() => setSearchFocused(false)}
                onInput={(event) => setSearchQuery(event.currentTarget.value)}
                onKeyDown={(event) => {
                  if (event.key === "Escape") {
                    event.preventDefault();
                    event.stopPropagation();
                    clearSearch();
                  }
                }}
              />
              <Show when={searchQuery()} fallback={<kbd>/</kbd>}>
                <button
                  type="button"
                  aria-label="Clear search"
                  onClick={clearSearch}
                >
                  <Icon name="close" size={14} />
                </button>
              </Show>
            </label>
          </div>
          <div class="topbar-right">
            <Show when={mode() === "live"}>
              <label class="unread-toggle">
                unread only{" "}
                <input
                  type="checkbox"
                  checked={unreadOnly()}
                  onChange={toggleUnread}
                />
                <i />
              </label>
            </Show>
            <button
              type="button"
              class="archive-toggle"
              classList={{ active: mode() === "archive" }}
              aria-pressed={mode() === "archive"}
              aria-label={
                mode() === "archive" ? "Return to live feed" : "Open archive"
              }
              onClick={() => void toggleArchive()}
            >
              <Icon name="archive" />
              <span>archive</span>
              <small>{heartCount()}</small>
            </button>
            <button
              type="button"
              class="keys-chip"
              onClick={() => setKeysOpen(true)}
            >
              {" "}
              ? keys
            </button>
            <button
              type="button"
              class="avatar"
              onClick={() => setView("feeds")}
              aria-label="Open feeds and account"
            >
              {initials()}
            </button>
          </div>
        </header>
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
          when={!loading() || items().length > 0}
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
            when={items().length > 0}
            fallback={
              mode() === "archive" ? (
                <ArchiveEmpty />
              ) : selectedTag() ? (
                <FilteredEmpty
                  tag={selectedTag()}
                  onClear={() => void applyTag("")}
                />
              ) : (
                <ColdStart onImport={() => setView("feeds")} />
              )
            }
          >
            <Grid
              items={gridItems()}
              layoutKey={layoutVersion()}
              scrollToTopKey={scrollTopVersion()}
              focusedID={focusedID()}
              active={
                !readerID() &&
                !keysOpen() &&
                !confirmRemove() &&
                !searchActive() &&
                !relatedSource()
              }
              hasMore={cursor() !== ""}
              archive={mode() === "archive"}
              linkActionID={linkActionID()}
              pendingNewCount={pendingNew().length}
              onFocus={setFocusedID}
              onOpen={markOpened}
              onSignal={setSignal}
              onHeart={toggleHeart}
              onToggleRead={toggleRead}
              onCopy={copyLink}
              onOriginal={openOriginal}
              onRelated={openRelated}
              onMarkBelow={markBelow}
              onItemsPassed={queueRead}
              onLoadMore={loadMore}
              onToggleOrder={toggleOrder}
              onToggleUnread={toggleUnread}
              onUndo={undoLast}
              onInsertNew={insertPendingNew}
              onRefresh={() => pollNew(true)}
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
                selectedIndex() >= 0 && selectedIndex() < gridItems().length - 1
              }
              onClose={() => setReaderID("")}
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
              onSignal={setSignal}
              onHeart={toggleHeart}
              onCopy={copyLink}
            />
          )}
        </Show>
        <Show when={keysOpen()}>
          <KeyboardMap onClose={() => setKeysOpen(false)} />
        </Show>
        <Show when={confirmRemove()}>
          {(item) => (
            <ConfirmRemove
              onCancel={() => setConfirmRemove()}
              onConfirm={() => {
                setConfirmRemove();
                void performHeart(item());
              }}
            />
          )}
        </Show>
        <Show when={toast()} keyed>
          {(notice) => (
            <div
              class="link-toast"
              classList={{ error: notice.kind === "error" }}
              role={notice.kind === "error" ? "alert" : "status"}
              aria-live={notice.kind === "error" ? "assertive" : "polite"}
            >
              <Icon
                name={notice.kind === "error" ? "close" : "check"}
                class="toast-icon"
              />
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
        <Icon name="back-to-grid" size={24} class="icon-quiet" />
      </div>
      <h1>Nothing scored yet.</h1>
      <p>
        Sema starts newest-first and sizes items by media and recency. Thumb a
        few dozen items and the grid begins shaping itself around what you
        actually read.
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
        Heart an item and it lands here permanently. Everything else in the feed
        expires after seven days; kept items don&apos;t.
      </p>
    </section>
  );
}

function FilteredEmpty(props: { tag: string; onClear(): void }) {
  return (
    <section class="archive-empty">
      <h1>No items in #{props.tag}</h1>
      <p>This tag has no visible items in the current seven-day window.</p>
      <button type="button" class="original-cta" onClick={props.onClear}>
        Show all feeds
      </button>
    </section>
  );
}
