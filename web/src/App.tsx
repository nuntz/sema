import { createMemo, createSignal, onCleanup, onMount, Show } from "solid-js";
import { APIClient, UnauthorizedError } from "./api/client";
import type { LayoutRow } from "./layout/justified";
import { chronoRead } from "./layout/read-state";
import type { Item, Order, Profile } from "./types";
import { Feeds } from "./ui/Feeds";
import { Grid } from "./ui/Grid";
import { KeyboardMap } from "./ui/KeyboardMap";
import { Reader } from "./ui/Reader";

type Undo =
  | { kind: "chrono"; boundary: string }
  | { kind: "interest"; ids: string[] };

export function App(props: { token: () => string; signOut(): void }) {
  const api = new APIClient(props.token);
  const [profile, setProfile] = createSignal<Profile>();
  const [signalCount, setSignalCount] = createSignal(0);
  const [order, setOrder] = createSignal<Order>("chrono");
  const [items, setItems] = createSignal<Item[]>([]);
  const [cursor, setCursor] = createSignal("");
  const [hasPage, setHasPage] = createSignal(false);
  const [loading, setLoading] = createSignal(true);
  const [loadingMore, setLoadingMore] = createSignal(false);
  const [error, setError] = createSignal("");
  const [unreadOnly, setUnreadOnly] = createSignal(false);
  const [focusedID, setFocusedID] = createSignal("");
  const [readerID, setReaderID] = createSignal("");
  const [hearted, setHearted] = createSignal(new Set<string>());
  const [keysOpen, setKeysOpen] = createSignal(false);
  const [view, setView] = createSignal<"grid" | "feeds">("grid");
  const [undo, setUndo] = createSignal<Undo>();
  let requestVersion = 0;
  let boundaryTimer: number | undefined;
  let interestTimer: number | undefined;
  const pendingInterest = new Set<string>();

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

  const reload = async (nextOrder = order()) => {
    const version = ++requestVersion;
    setLoading(true);
    setHasPage(false);
    setItems([]);
    setCursor("");
    try {
      const page = await api.items(nextOrder);
      if (version !== requestVersion) return;
      setItems(page.items);
      setCursor(page.next_cursor);
      setHasPage(true);
      setFocusedID(page.items[0]?.item_id ?? "");
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
    try {
      const page = await api.items(order(), nextCursor);
      if (nextCursor !== cursor()) return;
      setItems((current) => [
        ...current,
        ...page.items.filter(
          (item) =>
            !current.some((existing) => existing.item_id === item.item_id),
        ),
      ]);
      setCursor(page.next_cursor);
    } catch (caught) {
      handleError(caught);
    } finally {
      setLoadingMore(false);
    }
  };

  onMount(() => {
    bootstrap();
    const flush = () => {
      flushBoundary(true);
      flushInterest(true);
    };
    const onVisibility = () => {
      if (document.visibilityState === "hidden") flush();
    };
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("pagehide", flush);
    onCleanup(() => {
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("pagehide", flush);
      window.clearTimeout(boundaryTimer);
      window.clearTimeout(interestTimer);
    });
  });

  const shownItems = createMemo(() =>
    unreadOnly() ? items().filter((item) => !item.read) : items(),
  );
  const selected = createMemo(() =>
    items().find((item) => item.item_id === readerID()),
  );
  const selectedIndex = createMemo(() =>
    shownItems().findIndex((item) => item.item_id === readerID()),
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

  const applyBoundary = (boundary: string, remember = true) => {
    const previous = profile()?.read_boundary_ts ?? "";
    if (remember) setUndo({ kind: "chrono", boundary: previous });
    setProfile((current) =>
      current ? { ...current, read_boundary_ts: boundary } : current,
    );
    setItems((current) =>
      current.map((item) => ({
        ...item,
        read: chronoRead(item.published_ts, boundary),
      })),
    );
    window.clearTimeout(boundaryTimer);
    boundaryTimer = window.setTimeout(flushBoundary, 5_000);
  };

  const flushBoundary = (keepalive = false) => {
    window.clearTimeout(boundaryTimer);
    boundaryTimer = undefined;
    const boundary = profile()?.read_boundary_ts;
    if (boundary !== undefined)
      api.patchMe({ read_boundary_ts: boundary }, keepalive).catch(handleError);
  };

  const queueInterestRead = (ids: string[]) => {
    for (const id of ids) pendingInterest.add(id);
    setItems((current) =>
      current.map((item) =>
        pendingInterest.has(item.item_id) ? { ...item, read: true } : item,
      ),
    );
    window.clearTimeout(interestTimer);
    interestTimer = window.setTimeout(flushInterest, 5_000);
  };

  const flushInterest = (keepalive = false) => {
    window.clearTimeout(interestTimer);
    interestTimer = undefined;
    const ids = [...pendingInterest];
    if (ids.length === 0) return;
    pendingInterest.clear();
    setUndo({ kind: "interest", ids });
    const firstUnread = items().find((item) => !item.read);
    const position = firstUnread ? String(firstUnread.score) : "0";
    setProfile((current) =>
      current ? { ...current, interest_position: position } : current,
    );
    Promise.all([
      api.readBatch(ids, true, keepalive),
      api.patchMe({ interest_position: position }, keepalive),
    ]).catch(handleError);
  };

  const rowsPassed = (rows: LayoutRow[], boundary?: string) => {
    if (order() === "chrono" && boundary) applyBoundary(boundary);
    else
      queueInterestRead(
        rows.flatMap((row) => row.cells.map((cell) => cell.item.item_id)),
      );
  };

  const markOpened = (item: Item) => {
    if (!item.read) {
      if (order() === "chrono") applyBoundary(item.published_ts);
      else {
        replaceItem(item.item_id, { read: true });
        api.read(item.item_id, true).catch(handleError);
      }
    }
    setReaderID(item.item_id);
  };

  const toggleRead = (item: Item) => {
    if (order() === "interest") {
      replaceItem(item.item_id, { read: !item.read });
      api.read(item.item_id, !item.read).catch((caught) => {
        replaceItem(item.item_id, { read: item.read });
        handleError(caught);
      });
      return;
    }
    if (!item.read) {
      applyBoundary(item.published_ts);
      return;
    }
    const index = items().findIndex(
      (candidate) => candidate.item_id === item.item_id,
    );
    const newer = index > 0 ? items()[index - 1] : undefined;
    applyBoundary(newer?.published_ts ?? "");
  };

  const undoLast = () => {
    const operation = undo();
    if (!operation) return;
    setUndo(undefined);
    if (operation.kind === "chrono") {
      applyBoundary(operation.boundary, false);
      flushBoundary();
      return;
    }
    setItems((current) =>
      current.map((item) =>
        operation.ids.includes(item.item_id) ? { ...item, read: false } : item,
      ),
    );
    api.readBatch(operation.ids, false).catch(handleError);
  };

  const toggleOrder = async () => {
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
    const next = shownItems()[selectedIndex() + delta];
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
        <Feeds
          api={api}
          onBack={() => setView("grid")}
          onSignOut={props.signOut}
        />
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
            {shownItems().filter((item) => !item.read).length} unread ·{" "}
            {order() === "interest"
              ? `ranked from ${signalCount()} signals`
              : `${items().length} recent items`}
          </span>
          <span class="mobile-status">
            {shownItems().filter((item) => !item.read).length} unread
          </span>
          <div class="topbar-right">
            <label class="unread-toggle">
              unread only{" "}
              <input
                type="checkbox"
                checked={unreadOnly()}
                onChange={() => setUnreadOnly((value) => !value)}
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
              items={shownItems()}
              order={order()}
              boundary={profile()?.read_boundary_ts}
              interestPosition={profile()?.interest_position}
              focusedID={focusedID()}
              active={!readerID() && !keysOpen()}
              resetKey={`${order()}:${unreadOnly()}`}
              hasMore={cursor() !== ""}
              hearted={hearted()}
              onFocus={setFocusedID}
              onOpen={markOpened}
              onSignal={setSignal}
              onHeart={toggleHeart}
              onToggleRead={toggleRead}
              onRowsPassed={rowsPassed}
              onLoadMore={loadMore}
              onToggleOrder={toggleOrder}
              onToggleUnread={() => setUnreadOnly((value) => !value)}
              onUndo={undoLast}
              onKeys={() => setKeysOpen(true)}
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
              hearted={hearted().has(item().item_id)}
              canPrevious={selectedIndex() > 0}
              canNext={
                selectedIndex() >= 0 &&
                selectedIndex() < shownItems().length - 1
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
