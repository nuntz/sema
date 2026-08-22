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
import { justify, totalHeight, visibleRows } from "../layout/justified";
import { fullyPassedRows } from "../layout/read-state";
import type { Item } from "../types";
import { gridCommand } from "./keyboard";

interface GridProps {
  items: Item[];
  layoutKey: number;
  scrollToTopKey: number;
  focusedID: string;
  active: boolean;
  hasMore: boolean;
  hearted: Set<string>;
  onFocus(id: string): void;
  onOpen(item: Item): void;
  onSignal(item: Item, value: -1 | 0 | 1): void;
  onHeart(item: Item): void;
  onToggleRead(item: Item): void;
  onMarkBelow(item: Item): void;
  onItemsPassed(ids: string[]): void;
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
  const passedIDs = new Set<string>();

  const updateViewport = () => {
    setWidth(scroller.clientWidth);
    setViewportHeight(scroller.clientHeight);
  };

  const processScroll = () => {
    setScrollTop(scroller.scrollTop);
    const passed = fullyPassedRows(rows(), -1, scroller.scrollTop);
    const ids = passed.rows.flatMap((row) =>
      row.cells.flatMap((cell) => {
        const id = cell.item.item_id;
        if (passedIDs.has(id)) return [];
        passedIDs.add(id);
        return [id];
      }),
    );
    if (ids.length > 0) props.onItemsPassed(ids);
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
    props.scrollToTopKey;
    if (!scroller) return;
    scroller.scrollTop = 0;
    setScrollTop(0);
    passedIDs.clear();
  });

  createEffect(() => {
    if (props.items.length === 0 && props.hasMore) props.onLoadMore();
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
      case "mark-below":
        if (item) props.onMarkBelow(item);
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

  return (
    <div class="grid-scroll" ref={scroller} tabindex="-1">
      <div
        class="virtual-canvas"
        style={{ height: `${totalHeight(rows()) + 28}px` }}
      >
        <For each={visible()}>
          {(row) => (
            <div
              class="grid-row"
              style={{
                top: `${row.top + 14}px`,
                height: `${row.height}px`,
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
                        read: item().read,
                        "text-cell": !item().media_url,
                        [`size-${cell.effectiveSize.toLowerCase()}`]: true,
                      }}
                      style={{ width: `${cell.width}px` }}
                      data-item-id={item().item_id}
                      onMouseEnter={() => props.onFocus(item().item_id)}
                      onDblClick={() => props.onOpen(item())}
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
                      <div class="cell-actions">
                        <button
                          type="button"
                          classList={{ selected: item().signal === 1 }}
                          aria-label="Thumbs up"
                          onClick={() =>
                            props.onSignal(item(), item().signal === 1 ? 0 : 1)
                          }
                        >
                          ↑
                        </button>
                        <button
                          type="button"
                          classList={{ selected: item().signal === -1 }}
                          aria-label="Thumbs down"
                          onClick={() =>
                            props.onSignal(
                              item(),
                              item().signal === -1 ? 0 : -1,
                            )
                          }
                        >
                          ↓
                        </button>
                        <button
                          type="button"
                          class="heart"
                          classList={{
                            selected: props.hearted.has(item().item_id),
                          }}
                          aria-label="Heart"
                          onClick={() => props.onHeart(item())}
                        >
                          ♥
                        </button>
                      </div>
                      <button
                        type="button"
                        class="cell-main"
                        onClick={() => props.onOpen(item())}
                        aria-label={`Open ${item().title}`}
                      >
                        <div class="cell-copy">
                          <h2>{item().title}</h2>
                          <Show when={!item().media_url && item().summary}>
                            <p>{item().summary}</p>
                          </Show>
                          <div class="cell-meta">
                            <Favicon item={item()} />
                            <span>
                              {item().feed_title || "Feed"} ·{" "}
                              {relativeTime(item().published_ts)}
                            </span>
                            <Show when={!item().read}>
                              <b>{Math.round(item().score * 100)}</b>
                            </Show>
                          </div>
                        </div>
                      </button>
                    </article>
                  );
                }}
              </For>
            </div>
          )}
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
