import { batch, createSignal } from "solid-js";
import type { AppAPI } from "./api/client";
import { effectiveGridOrder } from "./grid-scope";
import type { ItemSession } from "./item-session";
import { excludeRenderedStoryItems, includeReadForGrid } from "./item-session";
import type { LayoutRow } from "./layout/justified";
import {
  gridReadStateContext,
  scrollReadCandidates,
} from "./layout/read-state";
import { PendingReads } from "./pending-reads";
import type { Item, Story } from "./types";
import { frontPageUnreadIDsAfter, mergeFrontPage } from "./ui/front-page";

type Undo = {
  ids: string[];
  gridSnapshot?: GridClearSnapshot & { count: number };
};

export interface ReadGeometry {
  rows: LayoutRow[];
  top: number;
  clientHeight: number;
  scrollHeight: number;
  userInitiated: boolean;
}
export function createReadState(
  api: Pick<AppAPI, "read" | "readBatch" | "behaviour" | "items">,
  session: ItemSession,
  handleError: (error: unknown) => void,
) {
  const {
    items,
    stories,
    mode,
    unreadOnly,
    scope,
    order,
    cursor,
    replaceItem,
    gridIDs,
    gridStoryIDs,
    focusedID,
    setGridIDs,
    setGridStoryIDs,
    setFocusedID,
    setScrollTarget,
    setLayoutVersion,
    setScrollTopVersion,
  } = session;
  const gridOrder = () => effectiveGridOrder(order(), scope());
  let disposed = false;
  let revision = 0;
  const [undo, setUndo] = createSignal<Undo>();
  const [readAdjust, setReadAdjust] = createSignal(0);
  let readTimer: ReturnType<typeof globalThis.setTimeout> | undefined;
  let undoTimer: ReturnType<typeof globalThis.setTimeout> | undefined;
  let undoDeadline = 0;
  let undoRemaining = 8_000;
  let undoHovered = false;
  let undoFocused = false;
  let markBelowInFlight = false;
  const pendingRead = new PendingReads();
  const readFlushes = new Set<Promise<void>>();

  const changeAdjust = (update: (value: number) => number) => {
    revision++;
    setReadAdjust(update);
  };
  const finishUndo = () => undo()?.gridSnapshot;
  const patchRead = (ids: Iterable<string>, read: boolean) =>
    batch(() => {
      for (const id of ids) replaceItem(id, { read });
    });
  const openStory = (story: Story) => {
    const lead = story.items[0];
    if (!lead) return;
    api.behaviour(lead.item_id, { opened: true }).catch(handleError);
    if (!story.items.some((item) => !item.read)) return;
    const version = session.version;
    const previous = new Map(
      story.items.map((item) => [item.item_id, changes.get(item.item_id)]),
    );
    // Let the Reader paint before updating every loaded Story copy.
    globalThis.requestAnimationFrame(() => {
      globalThis.requestAnimationFrame(() => {
        if (disposed || version !== session.version) return;
        const current = stories().find(
          (candidate) => candidate.story_id === story.story_id,
        );
        const unchanged = (current?.items ?? story.items).filter(
          (item) =>
            !item.read &&
            changes.get(item.item_id) === previous.get(item.item_id),
        );
        if (unchanged.length) void changeRead(unchanged, true);
      });
    });
  };
  const clearUndoTimer = () => {
    globalThis.clearTimeout(undoTimer);
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
    undoTimer = globalThis.setTimeout(expireFinishUndo, undoRemaining);
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

  const writeReadBatch = (ids: string[], read: boolean, keepalive = false) =>
    Promise.all(
      Array.from({ length: Math.ceil(ids.length / 100) }, (_, index) =>
        api.readBatch(
          ids.slice(index * 100, (index + 1) * 100),
          read,
          keepalive,
        ),
      ),
    ).then(() => {});

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
    changeAdjust((value) => value + unread.length);
    for (const id of unread) {
      pendingRead.add(id);
      changes.set(id, Symbol());
    }
    if (!gridSnapshot) clearUndoTimer();
    setUndo({
      ids: [...pendingRead],
      gridSnapshot: gridSnapshot
        ? { ...gridSnapshot, count: unread.length }
        : undefined,
    });
    if (gridSnapshot) startFinishUndoTimer();
    patchRead(requested, true);
    if (
      typeof document !== "undefined" &&
      document.visibilityState === "hidden"
    ) {
      void flushRead(true);
      return unread;
    }
    globalThis.clearTimeout(readTimer);
    readTimer = globalThis.setTimeout(() => void flushRead(), 5_000);
    return unread;
  };

  const flushRead = (keepalive = false) => {
    globalThis.clearTimeout(readTimer);
    readTimer = undefined;
    const ids = [...pendingRead];
    if (ids.length === 0) return Promise.resolve();
    setUndo((current) => (current?.gridSnapshot ? current : { ids }));
    const previousWrites = [...readFlushes];
    const operation = pendingRead
      .flush(async (batch) => {
        await Promise.all(previousWrites);
        await api.readBatch(batch, true, keepalive);
      })
      .catch((caught) => {
        handleError(caught);
        if (!disposed && pendingRead.size > 0 && readTimer === undefined) {
          readTimer = globalThis.setTimeout(() => void flushRead(), 5_000);
        }
      })
      .finally(() => {
        readFlushes.delete(operation);
      });
    readFlushes.add(operation);
    return operation;
  };

  const changes = new Map<string, symbol>();
  const changeRead = (members: Item[], read: boolean, single = false) => {
    const token = Symbol();
    const changed = members.filter((item) => item.read !== read);
    const previousWrites = [...readFlushes];
    for (const item of members) {
      pendingRead.delete(item.item_id);
      changes.set(item.item_id, token);
    }
    changeAdjust((value) => value + changed.length * (read ? 1 : -1));
    patchRead(
      members.map((item) => item.item_id),
      read,
    );
    const operation = Promise.all(previousWrites)
      .then(() =>
        single
          ? api.read(members[0].item_id, read)
          : writeReadBatch(
              members.map((item) => item.item_id),
              read,
            ),
      )
      .catch((caught) => {
        const rollback = changed.filter(
          (item) => changes.get(item.item_id) === token,
        );
        changeAdjust((value) => value - rollback.length * (read ? 1 : -1));
        batch(() => {
          for (const item of rollback)
            replaceItem(item.item_id, { read: item.read });
        });
        handleError(caught);
      })
      .finally(() => {
        for (const item of members)
          if (changes.get(item.item_id) === token) changes.delete(item.item_id);
        readFlushes.delete(operation);
      });
    readFlushes.add(operation);
    return operation;
  };
  const recordOpened = (item: Item, archive = item.archived === true) => {
    if (archive) return;
    api.behaviour(item.item_id, { opened: true }).catch(handleError);
    if (!item.read) void changeRead([item], true, true);
  };
  const toggleRead = (item: Item) => {
    if (mode() === "archive" || item.archived) return;
    return changeRead([item], !item.read, true);
  };
  const toggleStoryRead = (story: Story) => {
    if (mode() === "archive") return;
    return changeRead(
      story.items,
      story.items.some((item) => !item.read),
    );
  };

  const finishAndClear = (
    ids = [...items(), ...stories().flatMap((story) => story.items)]
      .filter((item) => !item.read)
      .map((item) => item.item_id),
  ) => {
    if (mode() !== "live" || !unreadOnly()) return;
    const cleared = finishAndClearGrid(
      gridIDs(),
      focusedID(),
      session.scrollTop,
    );
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
    session.invalidateGrid();
    session.scrollTop = 0;
    setGridIDs(cleared.ids);
    setGridStoryIDs([]);
    setFocusedID("");
    setScrollTarget(0);
    setLayoutVersion((value) => value + 1);
    setScrollTopVersion((value) => value + 1);
  };

  const markBelow = async (anchor: string) => {
    if (mode() === "archive" || markBelowInFlight) return;
    const loaded = anchor.startsWith("story:")
      ? gridOrder() === "interest" &&
        stories().some((story) => `story:${story.story_id}` === anchor)
      : items().some((item) => item.item_id === anchor) ||
        (gridOrder() === "interest" &&
          stories().some((story) =>
            story.items.some((item) => item.item_id === anchor),
          ));
    if (!loaded) return;
    markBelowInFlight = true;
    const version = session.version;
    const clearVersion = session.clearVersion;
    const markOrder = gridOrder();
    const ids =
      markOrder === "interest"
        ? frontPageUnreadIDsAfter(
            mergeFrontPage(stories(), items(), false),
            anchor,
          )
        : unreadIDsAfter(items(), anchor);
    let nextCursor = cursor();
    try {
      while (nextCursor !== "") {
        const page = await api.items(
          markOrder,
          nextCursor,
          includeReadForGrid(unreadOnly()),
          scope(),
          false,
          session.fetchWindow,
        );
        if (
          version !== session.version ||
          clearVersion !== session.clearVersion ||
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
      if (clearVersion === session.clearVersion) queueRead(ids);
    } catch (caught) {
      handleError(caught);
    } finally {
      markBelowInFlight = false;
    }
  };

  function undoLast() {
    const operation = undo();
    if (!operation) return;
    changeAdjust((value) => value - operation.ids.length);
    clearUndoTimer();
    undoRemaining = 8_000;
    setUndo(undefined);
    const unsent = new Set(
      operation.ids.filter((id) => {
        changes.set(id, Symbol());
        return pendingRead.delete(id);
      }),
    );
    if (pendingRead.size === 0) {
      globalThis.clearTimeout(readTimer);
      readTimer = undefined;
    }
    patchRead(operation.ids, false);
    if (operation.gridSnapshot && mode() === "live" && unreadOnly()) {
      session.invalidateGrid();
      session.scrollTop = operation.gridSnapshot.scrollTop;
      setGridIDs([...operation.gridSnapshot.ids]);
      setGridStoryIDs([...(operation.gridSnapshot.storyIDs ?? [])]);
      setFocusedID(operation.gridSnapshot.focusedID);
      setScrollTarget(operation.gridSnapshot.scrollTop);
      setLayoutVersion((value) => value + 1);
      setScrollTopVersion((value) => value + 1);
    }
    const sent = operation.ids.filter((id) => !unsent.has(id));
    if (sent.length > 0) {
      const prior = [...readFlushes];
      const operation = Promise.all(prior)
        .then(() => writeReadBatch(sent, false))
        .catch(handleError)
        .then(() => {})
        .finally(() => readFlushes.delete(operation));
      readFlushes.add(operation);
    }
  }

  let passedVersion = -1;
  let passedScrollVersion = -1;
  const passed = new Set<string>();
  const onPassed = (geometry: ReadGeometry) => {
    if (
      passedVersion !== session.version ||
      passedScrollVersion !== session.scrollTopVersion()
    ) {
      passed.clear();
      passedVersion = session.version;
      passedScrollVersion = session.scrollTopVersion();
    }
    const read = new Set(
      [...items(), ...stories().flatMap((story) => story.items)]
        .filter((item) => item.read)
        .map((item) => item.item_id),
    );
    const ids = scrollReadCandidates(
      gridReadStateContext(mode() === "archive", unreadOnly()),
      geometry.rows,
      geometry.top,
      geometry.clientHeight,
      geometry.scrollHeight,
      geometry.userInitiated,
      passed,
      read,
    );
    for (const id of ids) passed.add(id);
    if (ids.length) queueRead(ids);
  };
  return {
    onPassed,
    queueRead,
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
    markBelow,
    openStory,
    get revision() {
      return revision;
    },
    async settle() {
      await Promise.all(readFlushes);
      return pendingRead.size === 0;
    },
    pauseUndo(kind: "hover" | "focus", paused: boolean) {
      if (kind === "hover") undoHovered = paused;
      else undoFocused = paused;
      syncFinishUndoTimer();
    },
    dispose() {
      disposed = true;
      globalThis.clearTimeout(readTimer);
      clearUndoTimer();
    },
  };
}

export type GridClearSnapshot = {
  ids: string[];
  storyIDs?: string[];
  focusedID: string;
  scrollTop: number;
};

export function finishAndClearGrid(
  ids: string[],
  focusedID: string,
  scrollTop: number,
): { ids: string[]; snapshot: GridClearSnapshot } {
  return {
    ids: [],
    snapshot: {
      ids: [...ids],
      focusedID,
      scrollTop: Math.max(0, scrollTop),
    },
  };
}

export function updateRead(
  items: Item[],
  ids: Iterable<string>,
  read: boolean,
): Item[] {
  const selected = new Set(ids);
  let changed = false;
  const updated = items.map((item) => {
    if (!selected.has(item.item_id) || item.read === read) return item;
    changed = true;
    return { ...item, read };
  });
  return changed ? updated : items;
}

export function unreadIDsAfter(items: Item[], focusedID: string): string[] {
  const focused = items.findIndex((item) => item.item_id === focusedID);
  if (focused < 0) return [];
  return items
    .slice(focused + 1)
    .filter((item) => !item.read)
    .map((item) => item.item_id);
}

export function updateStoriesRead(
  stories: Story[],
  ids: Iterable<string>,
  read: boolean,
): Story[] {
  const changed = new Set(ids);
  if (changed.size === 0) return stories;
  let storiesChanged = false;
  const updated = stories.map((story) => {
    let storyChanged = false;
    const items = story.items.map((item) => {
      if (!changed.has(item.item_id) || item.read === read) return item;
      storyChanged = true;
      return { ...item, read };
    });
    if (!storyChanged) return story;
    storiesChanged = true;
    return { ...story, items };
  });
  return storiesChanged ? updated : stories;
}
