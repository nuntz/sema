import {
  createEffect,
  createMemo,
  createSignal,
  For,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import {
  justify,
  type LayoutRow,
  totalHeight,
  visibleRows,
} from "../layout/justified";
import { chronoBoundary, fullyPassedRows } from "../layout/read-state";
import type { Item, Order } from "../types";
import { gridCommand } from "./keyboard";

interface GridProps {
  items: Item[];
  order: Order;
  boundary?: string;
  interestPosition?: string;
  focusedID: string;
  active: boolean;
  resetKey: string;
  hasMore: boolean;
  hearted: Set<string>;
  onFocus(id: string): void;
  onOpen(item: Item): void;
  onSignal(item: Item, value: -1 | 0 | 1): void;
  onHeart(item: Item): void;
  onToggleRead(item: Item): void;
  onRowsPassed(rows: LayoutRow[], boundary?: string): void;
  onLoadMore(): void;
  onToggleOrder(): void;
  onToggleUnread(): void;
  onUndo(): void;
  onKeys(): void;
}

export function Grid(props: GridProps) {
  let scroller!: HTMLDivElement;
  let frame = 0;
  const [width, setWidth] = createSignal(0);
  const [viewportHeight, setViewportHeight] = createSignal(0);
  const [scrollTop, setScrollTop] = createSignal(0);
  const rows = createMemo(() =>
    justify(props.items, Math.max(0, width() - (width() < 700 ? 28 : 32))),
  );
  const visible = createMemo(() =>
    visibleRows(rows(), scrollTop(), viewportHeight()),
  );
  let lastPassed = -1;
  let restored = false;

  createEffect(() => {
    props.resetKey;
    lastPassed = -1;
    restored = false;
  });

  const updateViewport = () => {
    setWidth(scroller.clientWidth);
    setViewportHeight(scroller.clientHeight);
  };

  const processScroll = () => {
    setScrollTop(scroller.scrollTop);
    const passed = fullyPassedRows(rows(), lastPassed, scroller.scrollTop);
    if (passed.rows.length > 0) {
      lastPassed = passed.lastIndex;
      const finalRow = passed.rows.at(-1);
      const boundary =
        props.order === "chrono" && finalRow
          ? chronoBoundary(finalRow)
          : undefined;
      props.onRowsPassed(passed.rows, boundary);
    }
    if (
      scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight <
      scroller.clientHeight * 2
    )
      props.onLoadMore();
  };

  const onScroll = () => {
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(processScroll);
  };

  onMount(() => {
    const observer = new ResizeObserver(updateViewport);
    observer.observe(scroller);
    updateViewport();
    scroller.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("keydown", onKeyDown);
    onCleanup(() => {
      observer.disconnect();
      scroller.removeEventListener("scroll", onScroll);
      window.removeEventListener("keydown", onKeyDown);
      cancelAnimationFrame(frame);
    });
  });

  createEffect(() => {
    const availableRows = rows();
    if (restored || availableRows.length === 0 || width() === 0) return;
    let target: LayoutRow | undefined;
    const boundary = props.boundary;
    if (props.order === "chrono" && boundary)
      target = availableRows.find((row) =>
        row.cells.some((cell) => cell.item.published_ts <= boundary),
      );
    if (props.order === "interest" && props.interestPosition) {
      const score = Number(props.interestPosition);
      if (Number.isFinite(score))
        target = availableRows.find((row) =>
          row.cells.some((cell) => cell.item.score <= score),
        );
    }
    if (target) {
      scroller.scrollTop = Math.max(0, target.top - 20);
      restored = true;
    } else if (props.hasMore) {
      props.onLoadMore();
    } else {
      restored = true;
    }
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
    props.onFocus(id);
    requestAnimationFrame(() =>
      scroller
        .querySelector<HTMLElement>(`[data-item-id="${CSS.escape(id)}"]`)
        ?.scrollIntoView({ block: "nearest", inline: "nearest" }),
    );
  };

  const focused = () =>
    props.items.find((item) => item.item_id === props.focusedID) ??
    props.items[0];

  const onKeyDown = (event: KeyboardEvent) => {
    if (!props.active || event.metaKey || event.ctrlKey || event.altKey) return;
    const target = event.target as HTMLElement;
    if (target.matches("input, textarea, select")) return;
    const item = focused();
    const command = gridCommand(event.key);
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
        if (item) props.onSignal(item, item.signal === 1 ? 0 : 1);
        break;
      case "dislike":
        if (item) props.onSignal(item, item.signal === -1 ? 0 : -1);
        break;
      case "heart":
        if (item) props.onHeart(item);
        break;
      case "read":
        if (item) props.onToggleRead(item);
        break;
      case "undo":
        props.onUndo();
        break;
      case "original":
        if (item) window.open(item.url, "_blank", "noopener,noreferrer");
        break;
      case "order":
        props.onToggleOrder();
        break;
      case "unread":
        props.onToggleUnread();
        break;
      case "help":
        props.onKeys();
        break;
      default:
        return;
    }
    event.preventDefault();
  };

  const hasBoundary = (row: LayoutRow, rowIndex: number) => {
    const boundary = props.boundary;
    if (props.order !== "chrono" || !boundary) return false;
    const currentRead = row.cells.some(
      (cell) => cell.item.published_ts <= boundary,
    );
    const previousRead =
      rowIndex > 0 &&
      rows()[rowIndex - 1].cells.some(
        (cell) => cell.item.published_ts <= boundary,
      );
    return currentRead && !previousRead;
  };

  return (
    <div class="grid-scroll" ref={scroller} tabindex="-1">
      <div
        class="virtual-canvas"
        style={{ height: `${totalHeight(rows()) + 28}px` }}
      >
        <For each={visible()}>
          {(row) => {
            const rowIndex = () => rows().indexOf(row);
            return (
              <>
                <Show when={hasBoundary(row, rowIndex())}>
                  <div
                    class="read-boundary"
                    style={{ top: `${Math.max(0, row.top - 8)}px` }}
                  >
                    <i />
                    <span>
                      READ FROM HERE ↓ · press U to undo the last batch
                    </span>
                    <i />
                  </div>
                </Show>
                <div
                  class="grid-row"
                  style={{
                    top: `${row.top + 14}px`,
                    height: `${row.height}px`,
                  }}
                >
                  <For each={row.cells}>
                    {(cell) => (
                      <article
                        class="grid-cell"
                        classList={{
                          focused: cell.item.item_id === props.focusedID,
                          read: cell.item.read,
                          "text-cell": !cell.item.media_url,
                          [`size-${cell.effectiveSize.toLowerCase()}`]: true,
                        }}
                        style={{ width: `${cell.width}px` }}
                        data-item-id={cell.item.item_id}
                        onMouseEnter={() => props.onFocus(cell.item.item_id)}
                        onDblClick={() => props.onOpen(cell.item)}
                      >
                        <Show when={cell.item.media_url}>
                          <img
                            src={cell.item.media_url}
                            alt=""
                            loading="lazy"
                            width={cell.item.media_w}
                            height={cell.item.media_h}
                          />
                        </Show>
                        <div class="cell-scrim" />
                        <div class="cell-actions">
                          <button
                            type="button"
                            classList={{ selected: cell.item.signal === 1 }}
                            aria-label="Thumbs up"
                            onClick={() =>
                              props.onSignal(
                                cell.item,
                                cell.item.signal === 1 ? 0 : 1,
                              )
                            }
                          >
                            ↑
                          </button>
                          <button
                            type="button"
                            classList={{ selected: cell.item.signal === -1 }}
                            aria-label="Thumbs down"
                            onClick={() =>
                              props.onSignal(
                                cell.item,
                                cell.item.signal === -1 ? 0 : -1,
                              )
                            }
                          >
                            ↓
                          </button>
                          <button
                            type="button"
                            class="heart"
                            classList={{
                              selected: props.hearted.has(cell.item.item_id),
                            }}
                            aria-label="Heart"
                            onClick={() => props.onHeart(cell.item)}
                          >
                            ♥
                          </button>
                        </div>
                        <button
                          type="button"
                          class="cell-main"
                          onClick={() => props.onOpen(cell.item)}
                          aria-label={`Open ${cell.item.title}`}
                        >
                          <div class="cell-copy">
                            <h2>{cell.item.title}</h2>
                            <Show
                              when={!cell.item.media_url && cell.item.summary}
                            >
                              <p>{cell.item.summary}</p>
                            </Show>
                            <div class="cell-meta">
                              <Favicon item={cell.item} />
                              <span>
                                {cell.item.feed_title || "Feed"} ·{" "}
                                {relativeTime(cell.item.published_ts)}
                              </span>
                              <Show when={!cell.item.read}>
                                <b>{Math.round(cell.item.score * 100)}</b>
                              </Show>
                            </div>
                          </div>
                        </button>
                      </article>
                    )}
                  </For>
                </div>
              </>
            );
          }}
        </For>
      </div>
    </div>
  );
}

function Favicon(props: { item: Item }) {
  return (
    <Show
      when={props.item.favicon_url}
      fallback={
        <i class="favicon fallback">
          {(props.item.feed_title || "F").slice(0, 1).toUpperCase()}
        </i>
      }
    >
      <img
        class="favicon"
        src={props.item.favicon_url}
        alt=""
        width="11"
        height="11"
      />
    </Show>
  );
}

export function relativeTime(value: string): string {
  const seconds = Math.max(0, (Date.now() - new Date(value).getTime()) / 1000);
  if (seconds < 60) return "now";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}
