import {
  createEffect,
  createMemo,
  createSignal,
  For,
  onCleanup,
  onMount,
  Show,
  untrack,
} from "solid-js";
import { Portal } from "solid-js/web";
import { Icon } from "../components/Icon";
import { justify, totalHeight, visibleRows } from "../layout/justified";
import {
  fullyPassedRows,
  intersectingRowIDs,
  readStateEnabled,
  shouldLoadNextPage,
  shouldMarkAtBottom,
  shouldShowEndCard,
} from "../layout/read-state";
import { whyText } from "../ranking-display";
import type { Item } from "../types";
import { gridCommand } from "./keyboard";
import { PULL_THRESHOLD, RefreshGate, resistedPull } from "./pull-refresh";
import {
  beginLongPress,
  LONG_PRESS_MS,
  type LongPressGesture,
  longPressReady,
  moveLongPress,
} from "./touch-gestures";

interface GridProps {
  items: Item[];
  layoutKey: number;
  scrollToTopKey: number;
  focusedID: string;
  active: boolean;
  hasMore: boolean;
  archive: boolean;
  linkActionID: string;
  pendingNewCount: number;
  onFocus(id: string): void;
  onOpen(item: Item): void;
  onSignal(item: Item, value: -1 | 0 | 1): void;
  onHeart(item: Item): void;
  onToggleRead(item: Item): void;
  onCopy(item: Item): void;
  onOriginal(item: Item): void;
  onMarkBelow(item: Item): void;
  onItemsPassed(ids: string[]): void;
  onLoadMore(): void;
  onToggleOrder(): void;
  onToggleUnread(): void;
  onUndo(): void;
  onInsertNew(): void;
  onRefresh(): Promise<number>;
}

export function Grid(props: GridProps) {
  let scroller!: HTMLDivElement;
  let endButton!: HTMLButtonElement;
  let frame = 0;
  let programmaticFrame = 0;
  let scrollIdle: number | undefined;
  let goTimer: number | undefined;
  let longPressTimer: number | undefined;
  let longPress: LongPressGesture | undefined;
  let longPressItem: Item | undefined;
  let suppressOpenID = "";
  let sheetPanel!: HTMLElement;
  let sheetStartY = 0;
  let pullStartY = 0;
  let pullTracking = false;
  let pullWasReady = false;
  let refreshNoticeTimer: number | undefined;
  const refreshGate = new RefreshGate();
  let userScrolling = false;
  let programmaticScrolling = false;
  let endRequested = false;
  let goPending = false;
  const [width, setWidth] = createSignal(0);
  const [viewportHeight, setViewportHeight] = createSignal(0);
  const [scrollTop, setScrollTop] = createSignal(0);
  const [sheetItem, setSheetItem] = createSignal<Item>();
  const [sheetOffset, setSheetOffset] = createSignal(0);
  const [pullDistance, setPullDistance] = createSignal(0);
  const [refreshState, setRefreshState] = createSignal<
    "idle" | "pulling" | "ready" | "fetching" | "landed" | "up-to-date"
  >("idle");
  const [refreshCount, setRefreshCount] = createSignal(0);
  const liveItems = createMemo(
    () => new Map(props.items.map((item) => [item.item_id, item])),
  );
  const rows = createMemo(() => {
    props.layoutKey;
    return justify(
      untrack(() => props.items),
      Math.max(0, width() - (width() < 700 ? 28 : 32)),
    );
  });
  const visible = createMemo(() =>
    visibleRows(rows(), scrollTop(), viewportHeight()),
  );
  const unreadIDs = createMemo(() =>
    props.items.filter((item) => !item.read).map((item) => item.item_id),
  );
  const endTop = createMemo(() => totalHeight(rows()) + 28);
  const canvasHeight = createMemo(
    () => endTop() + (props.hasMore ? 0 : viewportHeight()),
  );
  const passedIDs = new Set<string>();

  const updateViewport = () => {
    setWidth(scroller.clientWidth);
    setViewportHeight(scroller.clientHeight);
  };

  const noteUserScroll = () => {
    cancelLongPress();
    if (programmaticScrolling) return;
    userScrolling = true;
    endRequested = false;
    window.clearTimeout(scrollIdle);
    scrollIdle = window.setTimeout(() => {
      userScrolling = false;
    }, 180);
  };

  const cancelLongPress = () => {
    window.clearTimeout(longPressTimer);
    longPressTimer = undefined;
    if (longPress) longPress.cancelled = true;
    longPress = undefined;
    longPressItem = undefined;
  };

  const openSheet = (item: Item) => {
    cancelLongPress();
    props.onFocus(item.item_id);
    setSheetOffset(0);
    setSheetItem(item);
    navigator.vibrate?.(10);
  };

  const closeSheet = () => {
    setSheetOffset(0);
    setSheetItem();
  };

  const runSheetAction = (action: () => void) => {
    closeSheet();
    action();
  };

  const startSheetDrag = (event: PointerEvent) => {
    if (event.pointerType === "touch") sheetStartY = event.clientY;
  };

  const moveSheetDrag = (event: PointerEvent) => {
    if (!sheetStartY || event.pointerType !== "touch") return;
    setSheetOffset(Math.max(0, event.clientY - sheetStartY));
  };

  const finishSheetDrag = () => {
    if (sheetOffset() > (sheetPanel?.clientHeight ?? 320) * 0.3) closeSheet();
    else setSheetOffset(0);
    sheetStartY = 0;
  };

  const clearRefreshNotice = () => {
    window.clearTimeout(refreshNoticeTimer);
    setRefreshCount(0);
    setRefreshState("idle");
    setPullDistance(0);
  };

  const runRefresh = () =>
    refreshGate.run(async () => {
      setRefreshState("fetching");
      setPullDistance(44);
      const count = await props.onRefresh();
      setRefreshCount(count);
      if (count > 0) {
        setRefreshState("landed");
        return count;
      }
      setRefreshState("up-to-date");
      refreshNoticeTimer = window.setTimeout(clearRefreshNotice, 1_400);
      return 0;
    });

  const onPullStart = (event: TouchEvent) => {
    if (
      props.archive ||
      event.touches.length !== 1 ||
      scroller.scrollTop > 0 ||
      refreshState() === "fetching"
    )
      return;
    pullStartY = event.touches[0].clientY;
    pullTracking = true;
    pullWasReady = false;
  };

  const onPullMove = (event: TouchEvent) => {
    if (!pullTracking || event.touches.length !== 1) return;
    const distance = resistedPull(event.touches[0].clientY - pullStartY);
    if (distance <= 0) return;
    event.preventDefault();
    setPullDistance(distance);
    const ready = distance >= PULL_THRESHOLD;
    setRefreshState(ready ? "ready" : "pulling");
    if (ready && !pullWasReady) navigator.vibrate?.(8);
    pullWasReady = ready;
  };

  const onPullEnd = () => {
    if (!pullTracking) return;
    pullTracking = false;
    pullStartY = 0;
    if (refreshState() === "ready") void runRefresh();
    else {
      setRefreshState("idle");
      setPullDistance(0);
    }
  };

  const startLongPress = (event: PointerEvent, item: Item) => {
    if (event.pointerType !== "touch") return;
    if ((event.target as HTMLElement).closest(".cell-actions")) return;
    cancelLongPress();
    longPress = beginLongPress(event.clientX, event.clientY, performance.now());
    longPressItem = item;
    longPressTimer = window.setTimeout(() => {
      if (
        longPress &&
        longPressItem &&
        longPressReady(longPress, performance.now())
      ) {
        suppressOpenID = longPressItem.item_id;
        openSheet(longPressItem);
      }
    }, LONG_PRESS_MS);
  };

  const moveLongPressGesture = (event: PointerEvent) => {
    if (longPress && moveLongPress(longPress, event.clientX, event.clientY))
      cancelLongPress();
  };

  const programmaticScroll = (action: () => void) => {
    userScrolling = false;
    programmaticScrolling = true;
    window.clearTimeout(scrollIdle);
    action();
    cancelAnimationFrame(programmaticFrame);
    programmaticFrame = requestAnimationFrame(() => {
      programmaticScrolling = false;
      userScrolling = false;
    });
  };

  const processScroll = () => {
    const top = scroller.scrollTop;
    setScrollTop(top);
    const ids = new Set<string>();
    if (
      readStateEnabled(props.archive) &&
      userScrolling &&
      !programmaticScrolling
    ) {
      const passed = fullyPassedRows(rows(), -1, top);
      for (const row of passed.rows) {
        for (const cell of row.cells) {
          const id = cell.item.item_id;
          if (passedIDs.has(id) || liveItems().get(id)?.read) continue;
          passedIDs.add(id);
          ids.add(id);
        }
      }
      if (
        shouldMarkAtBottom(
          true,
          top,
          scroller.clientHeight,
          scroller.scrollHeight,
        )
      ) {
        for (const id of intersectingRowIDs(
          rows(),
          top,
          scroller.clientHeight,
          14,
        )) {
          if (passedIDs.has(id) || liveItems().get(id)?.read) continue;
          passedIDs.add(id);
          ids.add(id);
        }
      }
    }
    if (ids.size > 0) props.onItemsPassed([...ids]);
    if (
      shouldLoadNextPage(
        props.hasMore,
        top,
        scroller.clientHeight,
        scroller.scrollHeight,
      )
    )
      props.onLoadMore();
  };

  const onScroll = () => {
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(processScroll);
    window.clearTimeout(scrollIdle);
    scrollIdle = window.setTimeout(() => {
      userScrolling = false;
    }, 180);
  };

  onMount(() => {
    const observer = new ResizeObserver(updateViewport);
    observer.observe(scroller);
    updateViewport();
    scroller.addEventListener("scroll", onScroll, { passive: true });
    scroller.addEventListener("wheel", noteUserScroll, { passive: true });
    scroller.addEventListener("touchmove", noteUserScroll, { passive: true });
    scroller.addEventListener("touchstart", onPullStart, { passive: true });
    scroller.addEventListener("touchmove", onPullMove, { passive: false });
    scroller.addEventListener("touchend", onPullEnd, { passive: true });
    scroller.addEventListener("touchcancel", onPullEnd, { passive: true });
    window.addEventListener("keydown", onKeyDown);
    onCleanup(() => {
      observer.disconnect();
      scroller.removeEventListener("scroll", onScroll);
      scroller.removeEventListener("wheel", noteUserScroll);
      scroller.removeEventListener("touchmove", noteUserScroll);
      scroller.removeEventListener("touchstart", onPullStart);
      scroller.removeEventListener("touchmove", onPullMove);
      scroller.removeEventListener("touchend", onPullEnd);
      scroller.removeEventListener("touchcancel", onPullEnd);
      window.removeEventListener("keydown", onKeyDown);
      cancelAnimationFrame(frame);
      cancelAnimationFrame(programmaticFrame);
      window.clearTimeout(scrollIdle);
      window.clearTimeout(goTimer);
      window.clearTimeout(longPressTimer);
      window.clearTimeout(refreshNoticeTimer);
    });
  });

  createEffect(() => {
    props.scrollToTopKey;
    if (!scroller) return;
    endRequested = false;
    programmaticScroll(() => {
      scroller.scrollTop = 0;
    });
    setScrollTop(0);
    passedIDs.clear();
  });

  createEffect(() => {
    if (props.items.length === 0 && props.hasMore) props.onLoadMore();
  });

  const continueToEnd = () => {
    if (!scroller || !endRequested) return;
    programmaticScroll(() => {
      scroller.scrollTop = scroller.scrollHeight;
    });
    if (props.hasMore) {
      props.onLoadMore();
      return;
    }
    endRequested = false;
    requestAnimationFrame(() => endButton?.focus({ preventScroll: true }));
  };

  createEffect(() => {
    props.layoutKey;
    props.hasMore;
    if (endRequested) requestAnimationFrame(continueToEnd);
  });

  const position = () => {
    const allRows = rows();
    const rowIndex = 0;
    const cellIndex = 0;
    for (let r = 0; r < allRows.length; r++) {
      const found = allRows[r].cells.findIndex(
        (cell) => cell.item.item_id === props.focusedID,
      );
      if (found >= 0) return { rowIndex: r, cellIndex: found };
    }
    return { rowIndex, cellIndex };
  };

  const move = (rowDelta: number, cellDelta: number) => {
    const allRows = rows();
    if (allRows.length === 0) return;
    const current = position();
    const rowIndex = Math.max(
      0,
      Math.min(allRows.length - 1, current.rowIndex + rowDelta),
    );
    const targetRow = allRows[rowIndex];
    const cellIndex = Math.max(
      0,
      Math.min(targetRow.cells.length - 1, current.cellIndex + cellDelta),
    );
    const id = targetRow.cells[cellIndex].item.item_id;
    endRequested = false;
    props.onFocus(id);
    requestAnimationFrame(() => {
      programmaticScroll(() =>
        scroller
          .querySelector<HTMLElement>(`[data-item-id="${CSS.escape(id)}"]`)
          ?.scrollIntoView({ block: "nearest", inline: "nearest" }),
      );
    });
  };

  const focused = () =>
    props.items.find((item) => item.item_id === props.focusedID) ??
    props.items[0];

  const clearGo = () => {
    goPending = false;
    window.clearTimeout(goTimer);
  };

  const goHome = () => {
    endRequested = false;
    const first = rows()[0]?.cells[0]?.item.item_id;
    if (first) props.onFocus(first);
    programmaticScroll(() => {
      scroller.scrollTop = 0;
      scroller.focus({ preventScroll: true });
    });
  };

  const goEnd = () => {
    endRequested = true;
    continueToEnd();
  };

  const markRemaining = () => {
    const ids = unreadIDs();
    for (const id of ids) passedIDs.add(id);
    props.onItemsPassed(ids);
  };

  const onKeyDown = (event: KeyboardEvent) => {
    if (sheetItem() && event.key === "Escape") {
      closeSheet();
      event.preventDefault();
      return;
    }
    if (sheetItem()) return;
    if (!props.active || event.metaKey || event.ctrlKey || event.altKey) return;
    const target = event.target as HTMLElement;
    if (target.matches("input, textarea, select")) return;
    if (target === endButton && (event.key === "Enter" || event.key === " "))
      return;
    const item = focused();
    const command = gridCommand(event.key);
    if (!command) {
      clearGo();
      if (
        ["PageDown", "PageUp", " "].includes(event.key) &&
        !(event.key === " " && target.matches("button, a"))
      )
        noteUserScroll();
      return;
    }
    if (command !== "go-prefix") clearGo();
    switch (command) {
      case "down":
        move(1, 0);
        break;
      case "up":
        move(-1, 0);
        break;
      case "left":
        move(0, -1);
        break;
      case "right":
        move(0, 1);
        break;
      case "open":
        if (item) props.onOpen(item);
        break;
      case "like":
        if (item && !props.archive)
          props.onSignal(item, item.signal === 1 ? 0 : 1);
        break;
      case "dislike":
        if (item && !props.archive)
          props.onSignal(item, item.signal === -1 ? 0 : -1);
        break;
      case "heart":
        if (item) props.onHeart(item);
        break;
      case "read":
        if (item && !props.archive) props.onToggleRead(item);
        break;
      case "mark-below":
        if (item && !props.archive) props.onMarkBelow(item);
        break;
      case "end":
        goEnd();
        break;
      case "home":
        goHome();
        break;
      case "go-prefix":
        if (goPending) {
          clearGo();
          goHome();
        } else {
          goPending = true;
          goTimer = window.setTimeout(clearGo, 600);
        }
        break;
      case "undo":
        if (!props.archive) props.onUndo();
        break;
      case "copy":
        if (item) props.onCopy(item);
        break;
      case "original":
        if (item) props.onOriginal(item);
        break;
      case "order":
        if (!props.archive) props.onToggleOrder();
        break;
      case "unread":
        if (!props.archive) props.onToggleUnread();
        break;
    }
    event.preventDefault();
  };

  return (
    <div class="grid-scroll" ref={scroller} tabindex="-1">
      <Show
        when={
          refreshState() !== "idle" ||
          (!props.archive && props.pendingNewCount > 0)
        }
      >
        <div
          class="pull-refresh"
          classList={{
            fetching: refreshState() === "fetching",
            ready: refreshState() === "ready",
          }}
          style={{ height: `${Math.max(pullDistance(), 44)}px` }}
        >
          <Show
            when={
              refreshState() === "pulling" ||
              refreshState() === "ready" ||
              refreshState() === "fetching"
            }
          >
            <svg viewBox="0 0 20 20" aria-label="Checking for new items">
              <circle
                cx="10"
                cy="10"
                r="8"
                style={{
                  "stroke-dashoffset": `${50.27 * (1 - Math.min(1, pullDistance() / PULL_THRESHOLD))}`,
                }}
              />
            </svg>
          </Show>
          <Show when={refreshState() === "up-to-date"}>
            <span>up to date</span>
          </Show>
          <Show
            when={
              refreshState() === "landed" ||
              (refreshState() === "idle" && props.pendingNewCount > 0)
            }
          >
            <button
              type="button"
              onClick={() => {
                if (refreshState() === "idle") props.onInsertNew();
                clearRefreshNotice();
              }}
            >
              {refreshState() === "landed"
                ? refreshCount()
                : props.pendingNewCount}{" "}
              new
            </button>
          </Show>
        </div>
      </Show>
      <div
        class="virtual-canvas"
        style={{
          height: `${canvasHeight()}px`,
          transform: `translateY(${pullDistance()}px)`,
        }}
      >
        <For each={visible()}>
          {(row) => (
            <div
              class="grid-row"
              style={{
                top: `${row.top + 14}px`,
                height: `${row.height}px`,
                gap: `${row.gap}px`,
              }}
            >
              <For each={row.cells}>
                {(cell) => {
                  const item = createMemo(
                    () => liveItems().get(cell.item.item_id) ?? cell.item,
                  );
                  return (
                    <article
                      class="grid-cell"
                      classList={{
                        focused: item().item_id === props.focusedID,
                        read: readStateEnabled(props.archive) && item().read,
                        "archive-cell": props.archive,
                        "text-cell": !item().media_url,
                        [`size-${cell.effectiveSize.toLowerCase()}`]: true,
                      }}
                      style={{ width: `${cell.width}px` }}
                      data-item-id={item().item_id}
                      onMouseEnter={() => props.onFocus(item().item_id)}
                      onDblClick={() => props.onOpen(item())}
                      onPointerDown={(event) => startLongPress(event, item())}
                      onPointerMove={moveLongPressGesture}
                      onPointerUp={cancelLongPress}
                      onPointerCancel={cancelLongPress}
                    >
                      <Show when={item().media_url}>
                        <img
                          src={item().media_url}
                          alt=""
                          loading="lazy"
                          width={item().media_w}
                          height={item().media_h}
                        />
                      </Show>
                      <div class="cell-scrim" />
                      <div class="cell-corner" aria-hidden="true">
                        <Show
                          when={props.archive}
                          fallback={
                            <span
                              class="cell-age"
                              title={publishedDate(item().published_ts)}
                            >
                              {relativeTime(item().published_ts)}
                            </span>
                          }
                        >
                          <span
                            class="cell-age"
                            title={publishedDate(
                              item().hearted_ts || item().published_ts,
                            )}
                          >
                            {`kept ${relativeTime(
                              item().hearted_ts || item().published_ts,
                            )}`}
                          </span>
                        </Show>
                        <Show when={!props.archive}>
                          <span class="cell-rank">
                            {rankBand(cell.effectiveSize)}
                          </span>
                        </Show>
                      </div>
                      <Show when={props.archive}>
                        <span class="kept-marker">
                          <Icon name="keep" size={14} filled={true} />
                        </span>
                      </Show>
                      <div class="cell-actions">
                        <button
                          type="button"
                          class="heart"
                          classList={{ selected: item().hearted }}
                          aria-label={
                            item().hearted
                              ? "Remove from archive"
                              : "Keep in archive"
                          }
                          aria-pressed={item().hearted}
                          onClick={() => props.onHeart(item())}
                        >
                          <Icon name="keep" size={14} filled={item().hearted} />
                        </button>
                        <button
                          type="button"
                          class="more"
                          aria-label="More actions"
                          aria-haspopup="dialog"
                          onClick={() => openSheet(item())}
                        >
                          <Icon name="more" size={14} />
                        </button>
                      </div>
                      <button
                        type="button"
                        class="cell-main"
                        onClick={() => {
                          if (suppressOpenID === item().item_id) {
                            suppressOpenID = "";
                            return;
                          }
                          props.onOpen(item());
                        }}
                        aria-label={`Open ${item().title}`}
                      >
                        <div class="cell-copy">
                          <h2>{item().title}</h2>
                          <Show
                            when={cell.effectiveSize !== "S" && item().summary}
                          >
                            <p>{item().summary}</p>
                          </Show>
                          <div class="cell-meta">
                            <Show when={props.archive && !item().has_body}>
                              <em>text only</em>
                            </Show>
                            <Show
                              when={props.archive}
                              fallback={
                                <span>{item().feed_title || "Feed"}</span>
                              }
                            >
                              <span>{item().feed_title || "Feed"}</span>
                            </Show>
                          </div>
                          <Show
                            when={!props.archive && cell.effectiveSize === "L"}
                          >
                            <div class="why-hint why-l" title={whyText(item())}>
                              {whyText(item())}
                            </div>
                          </Show>
                          <Show
                            when={
                              !props.archive &&
                              cell.effectiveSize === "M" &&
                              whyText(item())
                            }
                          >
                            <div class="why-hint why-m" title={whyText(item())}>
                              {whyText(item())}
                            </div>
                          </Show>
                        </div>
                      </button>
                    </article>
                  );
                }}
              </For>
            </div>
          )}
        </For>
        <Show when={shouldShowEndCard(props.hasMore)}>
          <section
            class="end-of-feed"
            style={{
              top: `${endTop()}px`,
              "min-height": `${viewportHeight()}px`,
            }}
          >
            <div>
              <Show
                when={props.archive}
                fallback={
                  <>
                    <h2>You&apos;re all caught up</h2>
                    <p>Everything currently loaded is behind you.</p>
                    <button
                      ref={endButton}
                      type="button"
                      onClick={markRemaining}
                    >
                      Mark remaining {unreadIDs().length} as read
                    </button>
                  </>
                }
              >
                <small>END OF ARCHIVE</small>
                <h2>That&apos;s everything you&apos;ve kept</h2>
                <p>
                  {props.items.length}{" "}
                  {props.items.length === 1 ? "item" : "items"}
                  {oldestHeartMonth(props.items)
                    ? `, oldest kept in ${oldestHeartMonth(props.items)}`
                    : ""}
                  . Nothing here expires.
                </p>
              </Show>
            </div>
          </section>
        </Show>
      </div>
      <Portal>
        <Show when={sheetItem()} keyed>
          {(item) => (
            <div
              class="action-sheet-layer"
              role="presentation"
              onPointerDown={(event) => {
                if (event.target === event.currentTarget) closeSheet();
              }}
            >
              <section
                ref={sheetPanel}
                class="action-sheet"
                role="dialog"
                aria-modal="true"
                aria-label={`Actions for ${item.title}`}
                style={{ transform: `translateY(${sheetOffset()}px)` }}
                onPointerDown={startSheetDrag}
                onPointerMove={moveSheetDrag}
                onPointerUp={finishSheetDrag}
                onPointerCancel={finishSheetDrag}
              >
                <i class="sheet-handle" aria-hidden="true" />
                <header>
                  <strong>{item.title}</strong>
                  <span>
                    {item.feed_title || "Feed"} ·{" "}
                    {relativeTime(item.published_ts)}
                  </span>
                </header>
                <button
                  type="button"
                  onClick={() => runSheetAction(() => props.onOriginal(item))}
                >
                  <Icon name="open-original" size={20} />
                  Open original
                </button>
                <button
                  type="button"
                  classList={{ selected: item.signal === 1 }}
                  onClick={() =>
                    runSheetAction(() =>
                      props.onSignal(item, item.signal === 1 ? 0 : 1),
                    )
                  }
                >
                  <Icon name="thumbs-up" size={20} filled={item.signal === 1} />
                  More like this
                </button>
                <button
                  type="button"
                  classList={{ selected: item.signal === -1 }}
                  onClick={() =>
                    runSheetAction(() =>
                      props.onSignal(item, item.signal === -1 ? 0 : -1),
                    )
                  }
                >
                  <Icon
                    name="thumbs-down"
                    size={20}
                    filled={item.signal === -1}
                  />
                  Less like this
                </button>
                <button
                  type="button"
                  classList={{ selected: item.hearted }}
                  onClick={() => runSheetAction(() => props.onHeart(item))}
                >
                  <Icon name="keep" size={20} filled={item.hearted} />
                  {item.hearted ? "Kept" : "Keep"}
                </button>
                <button
                  type="button"
                  onClick={() => runSheetAction(() => props.onCopy(item))}
                >
                  <Icon name="copy-link" size={20} />
                  Copy link
                </button>
              </section>
            </div>
          )}
        </Show>
      </Portal>
    </div>
  );
}

export function relativeTime(value: string): string {
  const seconds = Math.max(0, (Date.now() - new Date(value).getTime()) / 1000);
  if (seconds < 60) return "now";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}

function publishedDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(
    new Date(value),
  );
}

function rankBand(size: "S" | "M" | "L"): string {
  if (size === "L") return "top 10%";
  if (size === "M") return "top 40%";
  return "rest";
}

function oldestHeartMonth(items: Item[]): string {
  const value = items.at(-1)?.hearted_ts;
  if (!value) return "";
  return new Intl.DateTimeFormat(undefined, { month: "long" }).format(
    new Date(value),
  );
}
