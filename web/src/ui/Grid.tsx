import {
  createEffect,
  createMemo,
  createSignal,
  For,
  type JSX,
  on,
  onCleanup,
  onMount,
  Show,
  untrack,
} from "solid-js";
import { Portal } from "solid-js/web";
import { Icon } from "../components/Icon";
import {
  justify,
  type LayoutRow,
  reuseLayoutRows,
  totalHeight,
  visibleRows,
} from "../layout/justified";
import { type LayoutDirection, nearestCell } from "../layout/navigation";
import {
  caughtUpBoundary,
  caughtUpLabel,
  endMarkActionEnabled,
  gridReadStateContext,
  nextScrollReassert,
  readVisualState,
  type ScrollReassertState,
  scrollReadCandidates,
  shouldLoadNextPage,
  shouldLoadToFillViewport,
  shouldShowEndCard,
} from "../layout/read-state";
import { whyText } from "../ranking-display";
import { externalHost, isRedditItem, redditPrimaryRoute } from "../reddit-item";
import type { Item, Order, ReadAnchor, Story } from "../types";
import {
  frontPageSequence,
  horizontalStoryFocus,
  moveFrontPageFocus,
  type StoryBlockReadRow,
  storyScrollReadCandidates,
} from "./front-page";
import { gridCommand } from "./keyboard";
import { PULL_THRESHOLD, RefreshGate, resistedPull } from "./pull-refresh";
import { ResponsiveImage } from "./ResponsiveImage";
import { SourceBadge } from "./SourceBadge";
import {
  beginLongPress,
  LONG_PRESS_MS,
  type LongPressGesture,
  longPressReady,
  moveLongPress,
} from "./touch-gestures";

interface GridProps {
  items: Item[];
  lead?: JSX.Element;
  stories?: Story[];
  expandedStoryIDs?: ReadonlySet<string>;
  layoutKey: number;
  scrollToTopKey: number;
  scrollTarget: number;
  initialScrollTop?: number;
  focusedID: string;
  active: boolean;
  hasMore: boolean;
  archive: boolean;
  unreadOnly: boolean;
  order: Order;
  readStateItems: Item[];
  readAnchor?: ReadAnchor;
  linkActionID: string;
  pendingNewCount: number;
  onFocus(id: string): void;
  onOpen(item: Item): void;
  onOpenStoryLead?(story: Story): void;
  onExternalOpen(item: Item): void;
  onDiscussion(item: Item): void;
  onSignal(item: Item, value: -1 | 0 | 1): void;
  onHeart(item: Item): void;
  onToggleRead(item: Item): void;
  onToggleStoryRead?(story: Story): void;
  onCopy(item: Item): void;
  onOriginal(item: Item): void;
  onRelated(item: Item): void;
  onMarkBelow(item: Item): void;
  onMarkStoryBelow?(storyID: string): void;
  onItemsPassed(ids: string[]): void;
  onFinishAndClear(ids: string[]): void;
  onLoadMore(): void;
  onToggleOrder(): void;
  onUndo(): void;
  onRefresh(): Promise<number>;
  onScrollPosition?(top: number): void;
}

function shortWhyText(item: Item): string {
  if (item.why?.title) return `Liked: ${item.why.title}`;
  if (item.why?.feed_title) return `Often: ${item.why.feed_title}`;
  return "";
}

export function Grid(props: GridProps) {
  let scroller!: HTMLDivElement;
  let endButton!: HTMLButtonElement;
  let frame = 0;
  let programmaticFrame = 0;
  let restoreFrame = 0;
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
  let userScrollIntentVersion = 0;
  let endRequested = false;
  let goPending = false;
  const [width, setWidth] = createSignal(0);
  const [leadHeight, setLeadHeight] = createSignal(0);
  const [viewportHeight, setViewportHeight] = createSignal(0);
  const [scrollTop, setScrollTop] = createSignal(
    Math.max(0, props.initialScrollTop ?? 0),
  );
  const [sheetItem, setSheetItem] = createSignal<Item>();
  const [sheetOffset, setSheetOffset] = createSignal(0);
  const [pullDistance, setPullDistance] = createSignal(0);
  const [refreshState, setRefreshState] = createSignal<
    "idle" | "pulling" | "ready" | "fetching" | "landed" | "up-to-date"
  >("idle");
  const [refreshCount, setRefreshCount] = createSignal(0);
  let leadElement: HTMLDivElement | undefined;
  let leadObserver: ResizeObserver | undefined;
  const liveItems = createMemo(
    () => new Map(props.items.map((item) => [item.item_id, item])),
  );
  const readContext = createMemo(() =>
    gridReadStateContext(props.archive, props.unreadOnly),
  );
  const caughtUp = createMemo(() =>
    caughtUpBoundary(
      readContext(),
      props.order,
      props.readStateItems,
      props.items,
      props.readAnchor,
    ),
  );
  const contentWidth = createMemo(() =>
    Math.max(0, width() - (width() < 700 ? 24 : 32)),
  );
  const dividerHeight = createMemo(() => {
    if (contentWidth() < 310) return 40;
    return width() < 700 ? 24 : 28;
  });
  const layoutItems = createMemo(
    on(
      () => props.layoutKey,
      () => props.items,
    ),
  );
  let previousLayoutRows: LayoutRow[] = [];
  const stableRows = (next: LayoutRow[]) => {
    const rows = reuseLayoutRows(previousLayoutRows, next);
    previousLayoutRows = rows;
    return rows;
  };
  const layout = createMemo(() => {
    const items = layoutItems();
    const boundary = caughtUp();
    if (!boundary) {
      const rows = stableRows(justify(items, contentWidth(), props.hasMore));
      return { rows, height: totalHeight(rows) };
    }

    const beforeIndex = boundary.beforeItemID
      ? items.findIndex((item) => item.item_id === boundary.beforeItemID)
      : items.length;
    if (beforeIndex < 0) {
      const rows = stableRows(justify(items, contentWidth(), props.hasMore));
      return { rows, height: totalHeight(rows) };
    }

    const above = justify(items.slice(0, beforeIndex), contentWidth(), true, {
      completeSegment: true,
    });
    const aboveHeight = totalHeight(above);
    const below = justify(
      items.slice(beforeIndex),
      contentWidth(),
      props.hasMore,
    ).map((row) => ({
      ...row,
      top: row.top + aboveHeight + dividerHeight(),
    }));
    const rows = stableRows([...above, ...below]);
    return {
      rows,
      dividerTop: aboveHeight + 14,
      height:
        below.length > 0 ? totalHeight(rows) : aboveHeight + dividerHeight(),
    };
  });
  const rows = createMemo(() => layout().rows);
  const dividerTop = createMemo(() => layout().dividerTop);
  const visible = createMemo(() =>
    visibleRows(
      rows(),
      Math.max(0, scrollTop() - leadHeight()),
      viewportHeight(),
    ),
  );
  const storyList = createMemo(() => props.stories ?? []);
  const storyMemberIDs = createMemo(
    () =>
      new Map(
        storyList().map((story) => [
          story.story_id,
          story.items.map((item) => item.item_id),
        ]),
      ),
  );
  const frontSequence = createMemo(() =>
    frontPageSequence(
      storyList(),
      props.items,
      props.expandedStoryIDs ?? new Set<string>(),
    ),
  );
  const unreadIDs = createMemo(() =>
    props.items.filter((item) => !item.read).map((item) => item.item_id),
  );
  const showEndAction = createMemo(
    () => endMarkActionEnabled(readContext()) && props.items.length > 0,
  );
  const showEndMarkAction = createMemo(
    () => showEndAction() && unreadIDs().length > 0,
  );
  const gridEndTop = createMemo(() => {
    if (!props.archive && props.unreadOnly && props.items.length === 0)
      return 0;
    return layout().height + (!props.archive && props.unreadOnly ? 22 : 28);
  });
  const endTop = createMemo(() => leadHeight() + gridEndTop());
  const canvasHeight = createMemo(
    () => endTop() + (props.hasMore ? 0 : viewportHeight()),
  );
  const passedIDs = new Set<string>();

  const updateViewport = () => {
    setWidth(scroller.clientWidth);
    setViewportHeight(scroller.clientHeight);
  };

  const cancelScrollRestore = () => {
    cancelAnimationFrame(restoreFrame);
    restoreFrame = 0;
  };

  const noteUserScroll = () => {
    cancelLongPress();
    userScrollIntentVersion++;
    cancelScrollRestore();
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

  const reassertScrollTarget = (target: number) => {
    cancelScrollRestore();
    const intentVersion = userScrollIntentVersion;
    let state: ScrollReassertState = { frameCount: 0, stableFrames: 0 };
    const apply = () => {
      programmaticScroll(() => {
        scroller.scrollTop = target;
      });
      setScrollTop(scroller.scrollTop);
      props.onScrollPosition?.(scroller.scrollTop);
    };
    const verify = () => {
      const decision = nextScrollReassert(
        state,
        scroller.scrollTop === target,
        userScrollIntentVersion !== intentVersion,
      );
      state = decision.state;
      if (decision.reapply) apply();
      if (decision.scheduleNext) {
        restoreFrame = requestAnimationFrame(verify);
      } else {
        restoreFrame = 0;
      }
    };

    apply();
    restoreFrame = requestAnimationFrame(verify);
  };

  const processScroll = () => {
    const top = scroller.scrollTop;
    setScrollTop(top);
    props.onScrollPosition?.(top);
    const context = readContext();
    const userInitiated = userScrolling && !programmaticScrolling;
    const measureStoryRows =
      context === "unread" && userInitiated && storyList().length > 0;
    const scrollerTop = measureStoryRows
      ? scroller.getBoundingClientRect().top
      : 0;
    const membersByStoryID = measureStoryRows ? storyMemberIDs() : new Map();
    const storyRows: StoryBlockReadRow[] =
      measureStoryRows && leadElement
        ? Array.from(
            leadElement.querySelectorAll<HTMLElement>(".story-block-row"),
          ).map((row) => {
            const bounds = row.getBoundingClientRect();
            return {
              top: bounds.top - scrollerTop + top,
              bottom: bounds.bottom - scrollerTop + top,
              memberIDs: Array.from(
                row.querySelectorAll<HTMLElement>("[data-story-id]"),
              ).flatMap((card) =>
                card.dataset.storyId
                  ? (membersByStoryID.get(card.dataset.storyId) ?? [])
                  : [],
              ),
            };
          })
        : [];
    const storyReadIDs = storyScrollReadCandidates(
      context,
      storyRows,
      top,
      scroller.clientHeight,
      scroller.scrollHeight,
      userInitiated,
      passedIDs,
    );
    for (const id of storyReadIDs) passedIDs.add(id);
    const alreadyRead = new Set(
      props.items.filter((item) => item.read).map((item) => item.item_id),
    );
    const gridIDs = scrollReadCandidates(
      context,
      rows(),
      Math.max(0, top - leadHeight()),
      scroller.clientHeight,
      Math.max(0, scroller.scrollHeight - leadHeight()),
      userInitiated,
      passedIDs,
      alreadyRead,
    );
    const ids = [...storyReadIDs, ...gridIDs];
    for (const id of ids) passedIDs.add(id);
    if (ids.length > 0) props.onItemsPassed(ids);
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
    leadObserver = new ResizeObserver(() =>
      setLeadHeight(leadElement?.offsetHeight ?? 0),
    );
    observer.observe(scroller);
    if (leadElement) leadObserver.observe(leadElement);
    updateViewport();
    scroller.addEventListener("scroll", onScroll, { passive: true });
    scroller.addEventListener("wheel", noteUserScroll, { passive: true });
    scroller.addEventListener("touchmove", noteUserScroll, { passive: true });
    scroller.addEventListener("touchstart", onPullStart, { passive: true });
    scroller.addEventListener("touchmove", onPullMove, { passive: false });
    scroller.addEventListener("touchend", onPullEnd, { passive: true });
    scroller.addEventListener("touchcancel", onPullEnd, { passive: true });
    window.addEventListener("keydown", onKeyDown);
    const restoredTop = Math.max(0, props.initialScrollTop ?? 0);
    if (restoredTop > 0) {
      restoreFrame = requestAnimationFrame(() => {
        restoreFrame = 0;
        reassertScrollTarget(restoredTop);
      });
    }
    onCleanup(() => {
      props.onScrollPosition?.(scroller.scrollTop);
      observer.disconnect();
      leadObserver?.disconnect();
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
      cancelScrollRestore();
      window.clearTimeout(scrollIdle);
      window.clearTimeout(goTimer);
      window.clearTimeout(longPressTimer);
      window.clearTimeout(refreshNoticeTimer);
    });
  });

  createEffect(() => {
    props.lead;
    requestAnimationFrame(() => {
      leadObserver?.disconnect();
      if (leadElement) {
        leadObserver?.observe(leadElement);
        setLeadHeight(leadElement.offsetHeight);
      } else {
        setLeadHeight(0);
      }
    });
  });

  let currentScrollToTopKey = props.scrollToTopKey;
  createEffect(() => {
    const nextKey = props.scrollToTopKey;
    if (!scroller || nextKey === currentScrollToTopKey) return;
    currentScrollToTopKey = nextKey;
    endRequested = false;
    const target = Math.max(0, props.scrollTarget);
    reassertScrollTarget(target);
    passedIDs.clear();
  });

  createEffect(() => {
    const viewport = viewportHeight();
    // A tall story lead must not hide an empty or incomplete grid page. The
    // justified layout can withhold a trailing run until the next page arrives.
    if (shouldLoadToFillViewport(props.hasMore, gridEndTop(), viewport))
      props.onLoadMore();
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

  const move = (direction: LayoutDirection) => {
    const allRows = rows();
    if (allRows.length === 0) return;
    const id = nearestCell(allRows, props.focusedID, direction);
    if (!id) return;
    endRequested = false;
    props.onFocus(id);
    requestAnimationFrame(() => {
      programmaticScroll(() => {
        const cell = scroller.querySelector<HTMLElement>(
          `[data-item-id="${CSS.escape(id)}"]`,
        );
        cell?.scrollIntoView({ block: "nearest", inline: "nearest" });
        cell
          ?.querySelector<HTMLButtonElement>(".cell-main")
          ?.focus({ preventScroll: true });
      });
    });
  };

  const focusElement = (id: string) => {
    endRequested = false;
    props.onFocus(id);
    requestAnimationFrame(() => {
      programmaticScroll(() => {
        const target = scroller.querySelector<HTMLElement>(
          `[data-focus-id="${CSS.escape(id)}"], [data-item-id="${CSS.escape(id)}"]`,
        );
        target?.scrollIntoView({ block: "nearest", inline: "nearest" });
        const control = target?.matches("button, a")
          ? target
          : target?.querySelector<HTMLElement>(".story-lead, .cell-main");
        control?.focus({ preventScroll: true });
      });
    });
  };

  const moveFront = (delta: -1 | 1) => {
    const next = moveFrontPageFocus(frontSequence(), props.focusedID, delta);
    if (next) focusElement(next.id);
  };

  const focusedEntry = () =>
    frontSequence().find((entry) => entry.id === props.focusedID);

  const focused = () =>
    focusedEntry()?.item ??
    props.items.find((item) => item.item_id === props.focusedID) ??
    props.items[0];

  const focusedStory = () => {
    const storyID = focusedEntry()?.storyID;
    return storyID
      ? storyList().find((story) => story.story_id === storyID)
      : undefined;
  };

  const clearGo = () => {
    goPending = false;
    window.clearTimeout(goTimer);
  };

  const goHome = () => {
    endRequested = false;
    const first = frontSequence()[0]?.id ?? rows()[0]?.cells[0]?.item.item_id;
    if (first && storyList().length > 0) {
      programmaticScroll(() => {
        scroller.scrollTop = 0;
      });
      focusElement(first);
      return;
    }
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

  const finishAndClear = () => {
    const ids = unreadIDs();
    for (const id of ids) passedIDs.add(id);
    props.onFinishAndClear(ids);
    requestAnimationFrame(() => scroller.focus({ preventScroll: true }));
  };

  const openPrimary = (item: Item) => {
    const route = redditPrimaryRoute(item);
    if (route.kind === "external") {
      props.onExternalOpen(item);
      window.open(route.url, "_blank", "noopener,noreferrer");
      return;
    }
    props.onOpen(item);
  };

  const openFocused = (item: Item) => {
    const entry = focusedEntry();
    const story = focusedStory();
    const route = redditPrimaryRoute(item);
    if (entry?.kind === "story" && story && route.kind !== "external") {
      props.onOpenStoryLead?.(story);
      return;
    }
    openPrimary(item);
  };

  const openDiscussion = (item: Item) => {
    props.onDiscussion(item);
    window.open(item.url, "_blank", "noopener,noreferrer");
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
    if (
      target.closest(".story-heart, .story-more") &&
      (event.key === "Enter" || event.key === " ")
    )
      return;
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
        if (storyList().length > 0) moveFront(1);
        else move("down");
        break;
      case "up":
        if (storyList().length > 0) moveFront(-1);
        else move("up");
        break;
      case "left":
        if (focusedEntry()?.kind === "story") {
          const id = horizontalStoryFocus(
            storyList(),
            contentWidth(),
            props.focusedID,
            -1,
          );
          if (id) focusElement(id);
        } else if (focusedEntry()?.kind !== "headline") move("left");
        break;
      case "right":
        if (focusedEntry()?.kind === "story") {
          const id = horizontalStoryFocus(
            storyList(),
            contentWidth(),
            props.focusedID,
            1,
          );
          if (id) focusElement(id);
        } else if (focusedEntry()?.kind !== "headline") move("right");
        break;
      case "open":
        if (item) openFocused(item);
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
        if (item && !props.archive) {
          const story =
            focusedEntry()?.kind === "story" ? focusedStory() : undefined;
          if (story) props.onToggleStoryRead?.(story);
          else props.onToggleRead(item);
        }
        break;
      case "mark-below":
        if (item && !props.archive) {
          const storyID = focusedEntry()?.storyID;
          if (storyID) props.onMarkStoryBelow?.(storyID);
          else props.onMarkBelow(item);
        }
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
        if (item && isRedditItem(item)) openDiscussion(item);
        else if (item) props.onCopy(item);
        break;
      case "original":
        if (item) props.onOriginal(item);
        break;
      case "related":
        if (item) props.onRelated(item);
        break;
      case "order":
        if (!props.archive) props.onToggleOrder();
        break;
    }
    event.preventDefault();
  };

  return (
    <div class="grid-scroll" ref={scroller} tabindex="-1">
      <Show when={refreshState() !== "idle"}>
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
          <Show when={refreshState() === "landed"}>
            <button
              type="button"
              onClick={() => {
                clearRefreshNotice();
              }}
            >
              {refreshCount()} new
            </button>
          </Show>
        </div>
      </Show>
      <div
        class="virtual-canvas"
        style={{
          height: `${canvasHeight()}px`,
          transform:
            pullDistance() > 0 ? `translateY(${pullDistance()}px)` : undefined,
        }}
      >
        <Show when={props.lead}>
          <div class="grid-lead" ref={leadElement}>
            {props.lead}
          </div>
        </Show>
        <For each={visible()}>
          {(row) => (
            <div
              class="grid-row"
              style={{
                top: `${row.top + leadHeight() + 14}px`,
                height: `${row.height}px`,
              }}
            >
              <For each={row.cells}>
                {(cell) => {
                  const item = createMemo(
                    () => liveItems().get(cell.item.item_id) ?? cell.item,
                  );
                  const condensedLarge = createMemo(
                    () =>
                      width() < 700 &&
                      (row.kind === "hero" || row.kind === "pair"),
                  );
                  const explanation = createMemo(() =>
                    condensedLarge() ? shortWhyText(item()) : whyText(item()),
                  );
                  const readVisuals = createMemo(() =>
                    readVisualState(readContext(), item().read),
                  );
                  const primaryRoute = createMemo(() =>
                    redditPrimaryRoute(item()),
                  );
                  return (
                    <article
                      class="grid-cell"
                      classList={{
                        focused: item().item_id === props.focusedID,
                        read: readVisuals().dimmed,
                        "all-items-cell": readContext() === "all-items",
                        "archive-cell": props.archive,
                        "text-cell": !item().media_url,
                        "video-cell": item().media_type === "video",
                        "reddit-cell": isRedditItem(item()),
                        [`reddit-${item().post_type ?? "unknown"}`]:
                          isRedditItem(item()),
                        "span-2": cell.span === 2,
                        "tall-hero": cell.tall === true,
                        "hero-cell": row.kind === "hero",
                        "pair-cell": row.kind === "pair",
                        "sub-cell": row.kind === "span" && cell.span !== 2,
                        "compact-cell": row.kind === "compact",
                        [`size-${cell.effectiveSize.toLowerCase()}`]: true,
                      }}
                      style={{
                        left: `${cell.left}px`,
                        top: `${cell.offsetY ?? 0}px`,
                        width: `${cell.width}px`,
                        height: `${cell.height ?? row.height}px`,
                      }}
                      data-item-id={item().item_id}
                      onMouseEnter={() => props.onFocus(item().item_id)}
                      onDblClick={() => {
                        if (primaryRoute().kind !== "external")
                          openPrimary(item());
                      }}
                      onPointerDown={(event) => startLongPress(event, item())}
                      onPointerMove={moveLongPressGesture}
                      onPointerUp={cancelLongPress}
                      onPointerCancel={cancelLongPress}
                    >
                      <Show when={item().media_url}>
                        <ResponsiveImage
                          item={item()}
                          sizes={cell.width}
                          alt=""
                          loading="lazy"
                          width={item().media_w}
                          height={item().media_h}
                          onError={(event) =>
                            event.currentTarget
                              .closest(".grid-cell")
                              ?.classList.add("media-failed")
                          }
                        />
                      </Show>
                      <Show
                        when={
                          item().media_type === "video" ||
                          item().post_type === "video"
                        }
                      >
                        <span
                          class="video-play destination-glyph"
                          aria-hidden="true"
                        >
                          <Icon
                            name="play"
                            size={
                              row.kind === "hero" || row.kind === "pair"
                                ? 24
                                : cell.span === 2
                                  ? 20
                                  : cell.effectiveSize === "L"
                                    ? 15
                                    : cell.effectiveSize === "M"
                                      ? 14
                                      : 12
                            }
                            filled={true}
                          />
                        </span>
                      </Show>
                      <div class="cell-scrim" />
                      <div class="cell-corner" aria-hidden="true">
                        <UnreadDot visible={readVisuals().unreadDot} />
                        <div class="cell-corner-meta">
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
                      </div>
                      <Show when={item().hearted}>
                        <span class="kept-marker" aria-hidden="true">
                          <Icon name="keep" size={14} filled={true} />
                        </span>
                      </Show>
                      <Show
                        when={
                          primaryRoute().kind === "external" &&
                          item().external_url
                        }
                        fallback={
                          <button
                            type="button"
                            class="cell-main"
                            onClick={() => {
                              if (suppressOpenID === item().item_id) {
                                suppressOpenID = "";
                                return;
                              }
                              openPrimary(item());
                            }}
                            aria-label={`Open ${item().title}${readVisuals().unreadDot ? ", unread" : ""}`}
                          >
                            <CellCopy
                              item={item()}
                              archive={props.archive}
                              effectiveSize={cell.effectiveSize}
                              condensed={condensedLarge()}
                              explanation={explanation()}
                            />
                          </button>
                        }
                      >
                        <a
                          class="cell-main"
                          href={item().external_url}
                          target="_blank"
                          rel="noopener noreferrer"
                          onClick={() => props.onExternalOpen(item())}
                          aria-label={`Open ${item().title} on ${externalHost(item().external_url)}${readVisuals().unreadDot ? ", unread" : ""}`}
                        >
                          <CellCopy
                            item={item()}
                            archive={props.archive}
                            effectiveSize={cell.effectiveSize}
                            condensed={condensedLarge()}
                            explanation={explanation()}
                          />
                        </a>
                      </Show>
                      <Show when={isRedditItem(item())}>
                        <a
                          class="reddit-discussion"
                          href={item().url}
                          target="_blank"
                          rel="noopener noreferrer"
                          aria-label="Discussion on Reddit"
                          data-tooltip="Discussion on Reddit"
                          onClick={(event) => {
                            event.stopPropagation();
                            props.onDiscussion(item());
                          }}
                        >
                          <Icon name="discussion" size={13} />
                        </a>
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
                    </article>
                  );
                }}
              </For>
            </div>
          )}
        </For>
        <Show when={dividerTop()} keyed>
          {(top) => (
            <div
              class="caughtup-shell"
              style={{
                top: `${top + leadHeight()}px`,
                height: `${dividerHeight()}px`,
              }}
            >
              <div class="caughtup">
                <hr
                  class="caughtup-semantic"
                  aria-label={caughtUpLabel(caughtUp()?.count ?? 0)}
                />
                <i />
                <span class="caughtup-label" aria-hidden="true">
                  NEW SINCE YOU LAST CAUGHT UP{" "}
                  <span class="caughtup-separator">·</span>{" "}
                  <span class="caughtup-count">{caughtUp()?.count}</span>
                </span>
                <i />
              </div>
            </div>
          )}
        </Show>
        <Show when={shouldShowEndCard(props.hasMore)}>
          <section
            class="end-of-feed"
            classList={{
              "empty-grid": props.items.length === 0,
              "finish-card":
                !props.archive && props.unreadOnly && props.items.length > 0,
              "finish-card--all-read":
                !props.archive &&
                props.unreadOnly &&
                props.items.length > 0 &&
                unreadIDs().length === 0,
              "caughtup-empty":
                !props.archive && props.unreadOnly && props.items.length === 0,
              "caughtup-empty--pending":
                !props.archive &&
                props.unreadOnly &&
                props.items.length === 0 &&
                props.pendingNewCount > 0,
              "no-action": !showEndAction(),
            }}
            style={{
              top: `${endTop()}px`,
              "min-height": `${
                !props.archive && props.unreadOnly && props.items.length === 0
                  ? Math.max(viewportHeight(), width() < 520 ? 474 : 520)
                  : viewportHeight()
              }px`,
            }}
          >
            <div>
              <Show
                when={props.archive}
                fallback={
                  <Show
                    when={props.unreadOnly}
                    fallback={
                      <>
                        <h2>You&apos;re all caught up</h2>
                        <p>Everything currently loaded is behind you.</p>
                      </>
                    }
                  >
                    <Show
                      when={props.items.length > 0}
                      fallback={
                        <>
                          <i class="caughtup-empty__mark" aria-hidden="true" />
                          <h2>You&apos;re all caught up</h2>
                          <p>
                            Unread is empty. Anything that arrives from here on
                            shows up at the top.
                          </p>
                        </>
                      }
                    >
                      <Show
                        when={showEndMarkAction()}
                        fallback={
                          <div class="finish-card__copy finish-card__variant">
                            <h2>
                              <i class="finish-card__mark" aria-hidden="true" />
                              All caught up — everything here is already read
                            </h2>
                            <p>
                              Clearing marks nothing — it just empties the grid.
                              <span class="finish-card__hint">
                                {" "}
                                New arrivals come back as a pill at the top.
                              </span>
                            </p>
                          </div>
                        }
                      >
                        <div class="finish-card__copy finish-card__variant">
                          <h2>Everything loaded is behind you</h2>
                          <p>
                            {unreadIDs().length}{" "}
                            {unreadIDs().length === 1 ? "item is" : "items are"}{" "}
                            still unread. Clearing marks them and empties the
                            grid
                            <span class="finish-card__hint">
                              {" "}
                              — new arrivals come back as a pill at the top.
                            </span>
                          </p>
                        </div>
                      </Show>
                      <Show when={showEndAction()}>
                        <button
                          ref={endButton}
                          type="button"
                          onClick={finishAndClear}
                        >
                          <Show
                            when={showEndMarkAction()}
                            fallback={
                              <span class="finish-card__button-label">
                                Clear grid
                              </span>
                            }
                          >
                            <span class="finish-card__button-label">
                              Mark {unreadIDs().length} read &amp; clear
                            </span>
                          </Show>
                        </button>
                      </Show>
                    </Show>
                  </Show>
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
                <Show when={!isRedditItem(item)}>
                  <button
                    type="button"
                    onClick={() => runSheetAction(() => props.onOriginal(item))}
                  >
                    <Icon name="open-original" size={20} />
                    Open original
                  </button>
                </Show>
                <Show when={isRedditItem(item) && item.external_url}>
                  <button
                    type="button"
                    onClick={() =>
                      runSheetAction(() => {
                        props.onExternalOpen(item);
                        window.open(
                          item.external_url,
                          "_blank",
                          "noopener,noreferrer",
                        );
                      })
                    }
                  >
                    <Icon name="open-original" size={20} />
                    Open linked site
                  </button>
                </Show>
                <Show when={isRedditItem(item)}>
                  <button
                    type="button"
                    onClick={() => runSheetAction(() => openDiscussion(item))}
                  >
                    <Icon name="discussion" size={20} />
                    Open discussion
                  </button>
                </Show>
                <button
                  type="button"
                  classList={{ selected: item.signal === 1 }}
                  onClick={() =>
                    runSheetAction(() =>
                      props.onSignal(item, item.signal === 1 ? 0 : 1),
                    )
                  }
                >
                  <Icon name="boost" size={20} />
                  Boost
                </button>
                <button
                  type="button"
                  onClick={() => runSheetAction(() => props.onRelated(item))}
                >
                  <Icon name="search" size={20} />
                  Similar
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
                  <Icon name="bury" size={20} />
                  Bury
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

function CellCopy(props: {
  item: Item;
  archive: boolean;
  effectiveSize: "S" | "M" | "L";
  condensed: boolean;
  explanation: string;
}) {
  const reddit = () => isRedditItem(props.item);
  const domain = () =>
    props.item.post_type === "link"
      ? externalHost(props.item.external_url)
      : "";
  return (
    <div class="cell-copy">
      <h2>{props.item.title}</h2>
      <Show when={reddit() && domain()}>
        <div class="reddit-domain">{domain()}</div>
      </Show>
      <Show
        when={
          props.effectiveSize !== "S" &&
          !props.condensed &&
          props.item.summary &&
          (!reddit() || props.item.post_type === "text")
        }
      >
        <p>{props.item.summary}</p>
      </Show>
      <div class="cell-meta">
        <Show
          when={
            props.archive &&
            !props.item.has_body &&
            props.item.media_type !== "video"
          }
        >
          <em>text only</em>
        </Show>
        <SourceBadge
          connector={props.item.connector}
          imageURL={props.item.favicon_url}
          title={props.item.feed_title}
          size={16}
        />
        <span>
          {props.item.feed_title || "Feed"}
          <Show when={reddit()}>
            {` · ${relativeTime(props.item.published_ts)}`}
          </Show>
        </span>
      </div>
      <Show when={!props.archive && props.effectiveSize === "L"}>
        <div class="why-hint why-l" title={whyText(props.item)}>
          {props.explanation}
        </div>
      </Show>
      <Show
        when={
          !props.archive && props.effectiveSize === "M" && whyText(props.item)
        }
      >
        <div class="why-hint why-m" title={whyText(props.item)}>
          {whyText(props.item)}
        </div>
      </Show>
    </div>
  );
}

export function UnreadDot(props: { visible: boolean }) {
  let exitTimer: number | undefined;
  const [rendered, setRendered] = createSignal(props.visible);
  const [exiting, setExiting] = createSignal(false);

  createEffect(() => {
    const visible = props.visible;
    window.clearTimeout(exitTimer);
    if (visible) {
      setRendered(true);
      setExiting(false);
      return;
    }
    if (!untrack(rendered)) return;
    setExiting(true);
    exitTimer = window.setTimeout(() => {
      setRendered(false);
      setExiting(false);
    }, 140);
  });
  onCleanup(() => window.clearTimeout(exitTimer));

  return (
    <Show when={rendered()}>
      <span class="unread-dot" classList={{ exiting: exiting() }} />
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
