import { createMemo, createSignal, onCleanup, onMount, Show } from "solid-js";
import { APIClient, UnauthorizedError } from "./api/client";
import {
  mergeNewItems,
  pollCandidates,
  unreadIDsAfter,
  updateRead,
  visibleItemIDs,
} from "./item-list";
import type { Item, Order, Profile } from "./types";
import { Feeds } from "./ui/Feeds";
import { Grid } from "./ui/Grid";
import { KeyboardMap } from "./ui/KeyboardMap";
import { appCommand } from "./ui/keyboard";
import { Reader } from "./ui/Reader";

type Undo = { ids: string[] };

export function App(props: { token: () => string; signOut(): void }) {
  const api = new APIClient(props.token);
  const [profile, setProfile] = createSignal<Profile>();
  const [signalCount, setSignalCount] = createSignal(0);
  const [order, setOrder] = createSignal<Order>("chrono");
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
  const [hearted, setHearted] = createSignal(new Set<string>());
  const [keysOpen, setKeysOpen] = createSignal(false);
  const [view, setView] = createSignal<"grid" | "feeds">("grid");
  const [undo, setUndo] = createSignal<Undo>();
  let requestVersion = 0;
  let readTimer: number | undefined;
  let pollTimer: number | undefined;
  let pollInFlight = false;
  let markBelowInFlight = false;
  const pendingRead = new Set<string>();

  const handleError = (caught: unknown) => {
    if (caught instanceof UnauthorizedError) {
      props.signOut();
      return;
    }
    setError(
      caught instanceof Error ? caught.message : "Something went wrong.",
    );
  };

  const bootstrap = async () => {
    setLoading(true);
    setError("");
    try {
      const me = await api.me();
      setProfile(me.profile);
      setSignalCount(me.signal_count);
      setOrder(me.profile.order_pref || "chrono");
      await reload(me.profile.order_pref || "chrono");
    } catch (caught) {
      handleError(caught);
    } finally {
      setLoading(false);
    }
  };

  const reload = async (nextOrder = order(), nextUnreadOnly = unreadOnly()) => {
    const version = ++requestVersion;
    setLoading(true);
    setHasPage(false);
    setItems([]);
    setGridIDs([]);
    setPendingNew([]);
    setCursor("");
    try {
      const page = await api.items(nextOrder, "", !nextUnreadOnly);
      if (version !== requestVersion) return;
      const pageItems = page.items ?? [];
      setItems(pageItems);
      setGridIDs(visibleItemIDs(pageItems, nextUnreadOnly));
      setLayoutVersion((value) => value + 1);
      setScrollTopVersion((value) => value + 1);
      setCursor(page.next_cursor ?? "");
      setHasPage(true);
      setFocusedID(visibleItemIDs(pageItems, nextUnreadOnly)[0] ?? "");
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
      const page = await api.items(order(), nextCursor, !unreadOnly());
      if (nextCursor !== cursor()) return;
      let added: Item[] = [];
      setItems((current) => {
        const seen = new Set(current.map((item) => item.item_id));
        added = (page.items ?? []).filter((item) => !seen.has(item.item_id));
        return added.length > 0 ? [...current, ...added] : current;
      });
      const visible = visibleItemIDs(added, unreadOnly());
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

  const pollNew = async () => {
    if (
      pollInFlight ||
      loading() ||
      !hasPage() ||
      document.visibilityState !== "visible"
    )
      return;
    const version = requestVersion;
    pollInFlight = true;
    try {
      const page = await api.items("chrono");
      if (version !== requestVersion) return;
      const unseen = pollCandidates(
        items(),
        pendingNew(),
        page.items ?? [],
        unreadOnly(),
      );
      if (unseen.length > 0)
        setPendingNew((current) => mergeNewItems(current, unseen));
    } catch (caught) {
      handleError(caught);
    } finally {
      pollInFlight = false;
    }
  };

  onMount(() => {
    bootstrap();
    const flush = () => void flushRead(true);
    const onVisibility = () => {
      if (document.visibilityState === "hidden") flush();
      else void pollNew();
    };
    const onKeyDown = (event: KeyboardEvent) => {
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
      const target = event.target;
      if (
        target instanceof HTMLElement &&
        (target.isContentEditable || target.matches("input, textarea, select"))
      )
        return;
      event.preventDefault();
      setKeysOpen((open) => (command === "toggle-help" ? !open : false));
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
    });
  });

  const gridItems = createMemo(() => {
    const byID = new Map(items().map((item) => [item.item_id, item]));
    return gridIDs().flatMap((id) => {
      const item = byID.get(id);
      return item ? [item] : [];
    });
  });
  const selected = createMemo(() =>
    items().find((item) => item.item_id === readerID()),
  );
  const selectedIndex = createMemo(() =>
    gridItems().findIndex((item) => item.item_id === readerID()),
  );

  const replaceItem = (itemID: string, patch: Partial<Item>) => {
    setItems((current) =>
      current.map((item) =>
        item.item_id === itemID ? { ...item, ...patch } : item,
      ),
    );
  };

  const setSignal = (item: Item, value: -1 | 0 | 1) => {
    const previous = item.signal;
    replaceItem(item.item_id, { signal: value });
    api
      .signal(item.item_id, value)
      .then(() => {
        if (previous === 0 && value !== 0) setSignalCount((count) => count + 1);
        if (previous !== 0 && value === 0)
          setSignalCount((count) => Math.max(0, count - 1));
      })
      .catch((caught) => {
        replaceItem(item.item_id, { signal: previous });
        handleError(caught);
      });
  };

  const toggleHeart = (item: Item) =>
    setHearted((current) => {
      const next = new Set(current);
      if (next.has(item.item_id)) next.delete(item.item_id);
      else next.add(item.item_id);
      return next;
    });

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
    readTimer = window.setTimeout(() => void flushRead(), 5_000);
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

  const markOpened = (item: Item) => {
    if (!item.read) {
      replaceItem(item.item_id, { read: true });
      api.read(item.item_id, true).catch((caught) => {
        replaceItem(item.item_id, { read: false });
        handleError(caught);
      });
    }
    setReaderID(item.item_id);
  };

  const toggleRead = (item: Item) => {
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
    if (markBelowInFlight) return;
    markBelowInFlight = true;
    const version = requestVersion;
    const markOrder = order();
    const ids = unreadIDsAfter(items(), item.item_id);
    let nextCursor = cursor();
    try {
      while (nextCursor !== "") {
        const page = await api.items(markOrder, nextCursor, !unreadOnly());
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
    await flushRead();
    const next = !unreadOnly();
    setUnreadOnly(next);
    void reload(order(), next);
  };

  const insertPendingNew = () => {
    const incoming = pendingNew();
    if (incoming.length === 0) return;
    const known = new Set(items().map((item) => item.item_id));
    const added = incoming.filter((item) => !known.has(item.item_id));
    setPendingNew([]);
    if (added.length === 0) return;
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
  };

  const toggleOrder = async () => {
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
            onBack={() => setView("grid")}
            onKeys={() => setKeysOpen(true)}
            onSignOut={props.signOut}
          />
          <Show when={keysOpen()}>
            <KeyboardMap onClose={() => setKeysOpen(false)} />
          </Show>
        </>
      }
    >
      <main class="app-shell">
        <header class="topbar">
          <button
            type="button"
            class="wordmark"
            onClick={() => setView("grid")}
          >
            Sema
          </button>
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
          <span class="status-line">
            {items().filter((item) => !item.read).length} unread ·{" "}
            {order() === "interest"
              ? `ranked from ${signalCount()} signals`
              : `${items().length} recent items`}
          </span>
          <span class="mobile-status">
            {items().filter((item) => !item.read).length} unread
          </span>
          <div class="topbar-right">
            <label class="unread-toggle">
              unread only{" "}
              <input
                type="checkbox"
                checked={unreadOnly()}
                onChange={toggleUnread}
              />
              <i />
            </label>
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
        <Show when={pendingNew().length > 0}>
          <button
            type="button"
            class="new-items-pill"
            onClick={insertPendingNew}
          >
            {pendingNew().length} new
          </button>
        </Show>
        <Show when={error()}>
          <div class="error-banner" role="alert">
            <span>{error()}</span>
            <button type="button" onClick={() => setError("")}>
              ×
            </button>
          </div>
        </Show>
        <Show
          when={!loading() || items().length > 0}
          fallback={
            <div class="loading-screen">
              <i />
              <span>Loading your feed…</span>
            </div>
          }
        >
          <Show
            when={items().length > 0}
            fallback={<ColdStart onImport={() => setView("feeds")} />}
          >
            <Grid
              items={gridItems()}
              layoutKey={layoutVersion()}
              scrollToTopKey={scrollTopVersion()}
              focusedID={focusedID()}
              active={!readerID() && !keysOpen()}
              hasMore={cursor() !== ""}
              hearted={hearted()}
              onFocus={setFocusedID}
              onOpen={markOpened}
              onSignal={setSignal}
              onHeart={toggleHeart}
              onToggleRead={toggleRead}
              onMarkBelow={markBelow}
              onItemsPassed={queueRead}
              onLoadMore={loadMore}
              onToggleOrder={toggleOrder}
              onToggleUnread={toggleUnread}
              onUndo={undoLast}
            />
          </Show>
        </Show>
        <Show when={loadingMore()}>
          <div class="page-loader">fetching more…</div>
        </Show>
        <Show when={selected()}>
          {(item) => (
            <Reader
              item={item()}
              active={!keysOpen()}
              hearted={hearted().has(item().item_id)}
              canPrevious={selectedIndex() > 0}
              canNext={
                selectedIndex() >= 0 && selectedIndex() < gridItems().length - 1
              }
              onClose={() => setReaderID("")}
              onPrevious={() => moveReader(-1)}
              onNext={() => moveReader(1)}
              onSignal={(value) => setSignal(item(), value)}
              onHeart={() => toggleHeart(item())}
            />
          )}
        </Show>
        <Show when={keysOpen()}>
          <KeyboardMap onClose={() => setKeysOpen(false)} />
        </Show>
      </main>
    </Show>
  );
}

function ColdStart(props: { onImport(): void }) {
  return (
    <section class="cold-start">
      <div class="cold-mark">◲</div>
      <h1>Nothing scored yet.</h1>
      <p>
        Sema starts newest-first and sizes items by media and recency. Thumb a
        few dozen items and the grid begins shaping itself around what you
        actually read.
      </p>
      <button type="button" onClick={props.onImport}>
        Import OPML
      </button>
      <small>ranking activates at ~30 signals · 0 so far</small>
    </section>
  );
}
