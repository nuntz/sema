import {
  createEffect,
  createMemo,
  createSignal,
  For,
  on,
  onCleanup,
  onMount,
  Show,
  untrack,
  useContext,
} from "solid-js";
import { Portal } from "solid-js/web";
import { Icon } from "../components/Icon";
import {
  gridSourceName,
  headlineText,
  relatedCoverageHeight,
  repeatsLeadHeadline,
} from "../grid-display";
import type { ItemView } from "../item-view";
import {
  justify,
  type LayoutRow,
  reuseLayoutRows,
  totalHeight,
  visibleRows,
} from "../layout/justified";
import {
  cellRects,
  type LayoutDirection,
  nearestCell,
  nearestPageCell,
  nextGridPageTop,
} from "../layout/navigation";
import {
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
import type { ScopeCellModel } from "../scope-cell";
import { buryDisabled } from "../signal-feedback";
import type { FrontPageEntry, GridScope, Item, Order, Story } from "../types";
import { emptyState } from "./empty-state";
import { frontPageSequence } from "./front-page";
import {
  GridPixelRatioContext,
  gridImageOverscan,
  ImageLoadingEnabledContext,
} from "./image-loading";
import { gridCommand, isEditingTarget } from "./keyboard";
import { closeOverlay, pushOverlay } from "./overlay-history";
import { PULL_THRESHOLD, RefreshGate, resistedPull } from "./pull-refresh";
import { RelatedCoverage } from "./RelatedCoverage";
import { ResponsiveImage } from "./ResponsiveImage";
import { SignalActions } from "./SignalActions";
import {
  createSignalFresh,
  SignalLabel,
  SignalMarker,
  SignalWhy,
} from "./SignalState";
import { SourceBadge } from "./SourceBadge";
import { StoryCell } from "./StoryCell";
import { sheetHeadlineSlice } from "./story-layout";
import {
  beginLongPress,
  LONG_PRESS_MS,
  type LongPressGesture,
  longPressReady,
  moveLongPress,
} from "./touch-gestures";
import { useSheetDrag } from "./use-sheet-drag";

interface GridProps {
  scopeCell?: ScopeCellModel;
  scope?: GridScope;
  itemView?: ItemView;
  onClearScope?(): void;
  onShowAll?(): void;
  onShowRead?(): void;
  onOpenArchive?(): void;
  onSelectView?(view: "today" | "yesterday"): void;
  clearedCount?: number;
  items: Item[];
  entries: FrontPageEntry[];
  stories?: Story[];
  expandedStoryIDs?: ReadonlySet<string>;
  layoutKey: number;
  scrollToTopKey: number;
  scrollTarget: number;
  initialScrollTop?: number;
  focusedID: string;
  active: boolean;
  readerOpen: boolean;
  readerReveal: number;
  readerDragging: boolean;
  hasMore: boolean;
  archive: boolean;
  unreadOnly: boolean;
  order: Order;
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
  onApplyFeed(item: Item): void;
  onMarkBelow(item: Item): void;
  onMarkStoryBelow?(storyID: string): void;
  onExpandStory?(storyID: string): void;
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
  const imagesEnabled =
    new URLSearchParams(window.location.search).get("grid-images") !== "off";
  const [pixelRatio, setPixelRatio] = createSignal(
    window.devicePixelRatio || 1,
  );
  onMount(() => {
    if (!imagesEnabled) return;
    const updatePixelRatio = () => setPixelRatio(window.devicePixelRatio || 1);
    window.addEventListener("resize", updatePixelRatio);
    onCleanup(() => window.removeEventListener("resize", updatePixelRatio));
  });
  return (
    <ImageLoadingEnabledContext.Provider value={imagesEnabled}>
      <GridPixelRatioContext.Provider value={pixelRatio}>
        <GridContent {...props} />
      </GridPixelRatioContext.Provider>
    </ImageLoadingEnabledContext.Provider>
  );
}

function GridContent(props: GridProps) {
  const imagesEnabled = useContext(ImageLoadingEnabledContext);
  let scroller!: HTMLDivElement;
  let endButton!: HTMLButtonElement;
  let frame = 0;
  let programmaticFrame = 0;
  let restoreFrame = 0;
  let scrollIdle: number | undefined;
  let goTimer: number | undefined;
  let longPressTimer: number | undefined;
  let pressTimer: number | undefined;
  let longPress: LongPressGesture | undefined;
  let longPressItem: Item | undefined;
  let longPressStory: Story | undefined;
  let suppressOpenID = "";
  let sheetPanel!: HTMLElement;
  let pullStartY = 0;
  let pullTracking = false;
  let pullWasReady = false;
  let refreshNoticeTimer: number | undefined;
  const refreshGate = new RefreshGate();
  let userScrolling = false;
  let programmaticScrolling = false;
  let userScrollIntentVersion = 0;
  let endRequested = false;
  let pageFocus: { top: number; x: number; y: number } | undefined;
  let goPending = false;
  const [storyLeadHeights, setStoryLeadHeights] = createSignal(
    new Map<string, number>(),
  );
  const recordStoryLeadHeight = (storyID: string, height: number) => {
    setStoryLeadHeights((previous) => {
      if (previous.get(storyID) === height) return previous;
      return new Map(previous).set(storyID, height);
    });
  };
  const [width, setWidth] = createSignal(0);
  const [viewportHeight, setViewportHeight] = createSignal(0);
  const [scrollTop, setScrollTop] = createSignal(
    Math.max(0, props.initialScrollTop ?? 0),
  );
  const [sheetItem, setSheetItem] = createSignal<Item>();
  const [sheetStory, setSheetStory] = createSignal<Story>();
  const [pressedID, setPressedID] = createSignal("");
  const [pullDistance, setPullDistance] = createSignal(0);
  const [refreshState, setRefreshState] = createSignal<
    "idle" | "pulling" | "ready" | "fetching" | "landed" | "up-to-date"
  >("idle");
  const [refreshCount, setRefreshCount] = createSignal(0);
  const liveItems = createMemo(
    () =>
      new Map(
        [
          ...props.items,
          ...(props.stories ?? []).flatMap((story) => story.items),
        ].map((item) => [item.item_id, item]),
      ),
  );
  const readContext = createMemo(() =>
    gridReadStateContext(props.archive, props.unreadOnly),
  );
  const contentWidth = createMemo(() =>
    Math.max(0, width() - (width() < 700 ? 24 : 32)),
  );
  const mobile = () => contentWidth() < 700;
  const [keyboardFocus, setKeyboardFocus] = createSignal(false);
  const storyHeadlineHeights = createMemo(
    () =>
      new Map(
        (props.stories ?? []).flatMap((story) =>
          story.items
            .slice(1)
            .map(
              (item) =>
                [
                  item.item_id,
                  relatedCoverageHeight(story.items[0], item) +
                    (mobile() ? 12 : 0),
                ] as const,
            ),
        ),
      ),
  );
  const layoutSnapshot = createMemo(
    on(
      () => props.layoutKey,
      () => ({ entries: props.entries, hasMore: props.hasMore }),
    ),
  );
  let previousLayoutRows: LayoutRow[] = [];
  const stableRows = (next: LayoutRow[]) => {
    const rows = reuseLayoutRows(previousLayoutRows, next);
    previousLayoutRows = rows;
    return rows;
  };
  const layout = createMemo(() => {
    const { entries, hasMore } = layoutSnapshot();
    const rows = stableRows(
      justify(entries, contentWidth(), hasMore, {
        expandedStoryIDs: props.expandedStoryIDs,
        storyLeadHeights: storyLeadHeights(),
        storyHeadlineHeights: storyHeadlineHeights(),
        storyCardMinHeight: mobile() ? undefined : 310,
        refinedMobile: true,
      }),
    );
    return { rows, height: totalHeight(rows) };
  });
  const scopeCellHeight = createMemo(() =>
    props.scopeCell ? 44 + (mobile() ? 8 : 10) : 0,
  );
  const rows = createMemo<LayoutRow[]>((previous) =>
    reuseLayoutRows(
      previous ?? [],
      layout().rows.map((row) => ({
        ...row,
        top: row.top + scopeCellHeight(),
      })),
    ),
  );
  const closedEmpty = createMemo(() =>
    !props.archive && props.entries.length === 0
      ? emptyState({
          ...props,
          scopeTitle: props.scopeCell?.title,
          phone: mobile(),
        })
      : undefined,
  );
  const visible = createMemo(() =>
    // Keep a small buffer shared with image prefetching.
    visibleRows(
      rows(),
      scrollTop(),
      viewportHeight(),
      gridImageOverscan(viewportHeight()),
    ),
  );
  const storyList = createMemo(() => props.stories ?? []);
  const liveStories = createMemo(
    () => new Map(storyList().map((story) => [story.story_id, story])),
  );
  const frontSequence = createMemo(() =>
    frontPageSequence(
      props.entries,
      props.expandedStoryIDs ?? new Set<string>(),
    ),
  );
  const unreadIDs = createMemo(() =>
    Array.from(liveItems().values())
      .filter((item) => !item.read)
      .map((item) => item.item_id),
  );
  const showEndAction = createMemo(
    () => endMarkActionEnabled(readContext()) && props.entries.length > 0,
  );
  const showEndMarkAction = createMemo(
    () => showEndAction() && unreadIDs().length > 0,
  );
  const gridEndTop = createMemo(() => {
    if (closedEmpty()?.centered) return scopeCellHeight();
    if (closedEmpty()) return (props.scopeCell ? 44 : 0) + 26;
    return (
      scopeCellHeight() +
      layout().height +
      (!props.archive && props.unreadOnly ? 22 : 28)
    );
  });
  const endTop = createMemo(() => gridEndTop());
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
    pageFocus = undefined;
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
    window.clearTimeout(pressTimer);
    longPressTimer = undefined;
    pressTimer = undefined;
    if (longPress) longPress.cancelled = true;
    longPress = undefined;
    longPressItem = undefined;
    longPressStory = undefined;
    setPressedID("");
  };

  const openSheet = (item: Item, story?: Story) => {
    cancelLongPress();
    props.onFocus(story ? `story:${story.story_id}` : item.item_id);
    if (!sheetItem())
      pushOverlay("action-sheet", () => {
        setSheetItem();
        setSheetStory();
      });
    setSheetItem(item);
    setSheetStory(story);
    navigator.vibrate?.(10);
  };

  const closeSheet = () => {
    if (!sheetItem()) return;
    closeOverlay("action-sheet");
    setSheetItem();
    setSheetStory();
  };

  const runSheetAction = (action: () => void) => {
    closeSheet();
    action();
  };

  const sheetDrag = useSheetDrag({
    panel: () => sheetPanel,
    onDismiss: closeSheet,
    scrollTop: (target) => {
      if (!(target instanceof Element)) return;
      return target.closest<HTMLElement>(".sheet-headlines")?.scrollTop;
    },
  });

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

  const startLongPress = (event: PointerEvent, item: Item, story?: Story) => {
    if (event.pointerType !== "touch") return;
    if ((event.target as HTMLElement).closest(".cell-actions")) return;
    cancelLongPress();
    longPress = beginLongPress(event.clientX, event.clientY, performance.now());
    longPressItem = item;
    longPressStory = story;
    const id = story ? `story:${story.story_id}` : item.item_id;
    pressTimer = window.setTimeout(() => {
      if (longPress && !longPress.cancelled) setPressedID(id);
    }, 80);
    longPressTimer = window.setTimeout(() => {
      if (
        longPress &&
        longPressItem &&
        longPressReady(longPress, performance.now())
      ) {
        suppressOpenID = longPressItem.item_id;
        openSheet(longPressItem, longPressStory);
      }
    }, LONG_PRESS_MS);
  };

  const moveLongPressGesture = (event: PointerEvent) => {
    if (longPress && moveLongPress(longPress, event.clientX, event.clientY))
      cancelLongPress();
  };

  const programmaticScroll = (action: () => void) => {
    pageFocus = undefined;
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
    const alreadyRead = new Set(
      Array.from(liveItems().values())
        .filter((item) => item.read)
        .map((item) => item.item_id),
    );
    const ids = scrollReadCandidates(
      context,
      rows(),
      top,
      scroller.clientHeight,
      scroller.scrollHeight,
      userInitiated,
      passedIDs,
      alreadyRead,
    );
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
    if (
      pageFocus &&
      Math.abs(
        top -
          Math.min(
            pageFocus.top,
            scroller.scrollHeight - scroller.clientHeight,
          ),
      ) < 1
    )
      finishPageFocus();
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
      window.clearTimeout(pressTimer);
      window.clearTimeout(refreshNoticeTimer);
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
    const rect = cellRects(allRows).find((candidate) => candidate.id === id);
    endRequested = false;
    props.onFocus(id);
    programmaticScroll(() => {
      if (!rect) return;
      const top = rect.top + 14;
      const bottom = rect.bottom + 14;
      if (top < scroller.scrollTop) scroller.scrollTop = top;
      else if (bottom > scroller.scrollTop + scroller.clientHeight)
        scroller.scrollTop = Math.min(top, bottom - scroller.clientHeight);
      // Mount a destination outside the current virtual window before focusing it.
      setScrollTop(scroller.scrollTop);
    });
    requestAnimationFrame(() => {
      focusControl(id)?.focus({ preventScroll: true });
    });
  };

  const focusControl = (id: string) => {
    const target = scroller.querySelector<HTMLElement>(
      `[data-focus-id="${CSS.escape(id)}"], [data-item-id="${CSS.escape(id)}"]`,
    );
    return target?.matches("button, a")
      ? target
      : target?.querySelector<HTMLElement>(".story-lead, .cell-main");
  };

  const finishPageFocus = () => {
    const pending = pageFocus;
    pageFocus = undefined;
    if (!pending || !props.active || sheetItem()) return;
    const viewport = scroller.getBoundingClientRect();
    const controls = Array.from(
      scroller.querySelectorAll<HTMLElement>(
        ".cell-main, .story-lead, .story-headline",
      ),
    ).filter((control) => control.getClientRects().length > 0);
    const rects = controls.map((control) => {
      const owner = control.closest<HTMLElement>(
        "[data-focus-id], [data-item-id]",
      );
      const rect = control.getBoundingClientRect();
      return {
        id: owner?.dataset.focusId ?? owner?.dataset.itemId ?? "",
        left: rect.left,
        right: rect.right,
        top: rect.top,
        bottom: rect.bottom,
        centerX: (rect.left + rect.right) / 2,
        centerY: (rect.top + rect.bottom) / 2,
      };
    });
    const id = nearestPageCell(
      rects,
      viewport.left + pending.x,
      viewport.top + pending.y,
      viewport.top,
      viewport.bottom,
    );
    if (!id) return;
    props.onFocus(id);
    focusControl(id)?.focus({ preventScroll: true });
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
        const control = focusControl(id);
        control?.focus({ preventScroll: true });
      });
    });
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
    if (first?.startsWith("story:")) {
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

  const openStoryLead = (story: Story) => {
    const lead = story.items[0];
    if (!lead) return;
    if (suppressOpenID === lead.item_id) {
      suppressOpenID = "";
      return;
    }
    props.onOpenStoryLead?.(story);
  };

  const openFocused = (item: Item) => {
    const entry = focusedEntry();
    const story = focusedStory();
    const route = redditPrimaryRoute(item);
    if (entry?.kind === "story" && story && route.kind !== "external") {
      openStoryLead(story);
      return;
    }
    openPrimary(item);
  };

  const openDiscussion = (item: Item) => {
    props.onDiscussion(item);
    window.open(item.url, "_blank", "noopener,noreferrer");
  };

  const onKeyDown = (event: KeyboardEvent) => {
    if (
      event.defaultPrevented ||
      event.isComposing ||
      isEditingTarget(event.target)
    ) {
      clearGo();
      return;
    }
    setKeyboardFocus(true);
    if (sheetItem() && event.key === "Escape") {
      closeSheet();
      event.preventDefault();
      return;
    }
    if (sheetItem()) return;
    if (!props.active || event.metaKey || event.ctrlKey || event.altKey) return;
    const target = event.target as HTMLElement;
    if (target.isContentEditable || target.matches("input, textarea, select"))
      return;
    const control = target.closest("button, a, summary");
    if (
      event.key === " " &&
      control &&
      !control.matches(".cell-main, .story-lead, .story-headline")
    )
      return;
    if (
      target.closest(
        ".story-heart, .story-more, .story-more-action, .cell-actions",
      ) &&
      (event.key === "Enter" || event.key === " ")
    )
      return;
    if (target === endButton && (event.key === "Enter" || event.key === " "))
      return;
    const item = focused();
    const command = gridCommand(event.key);
    if (command !== "page-down" && command !== "page-up") pageFocus = undefined;
    if (!command) {
      clearGo();
      return;
    }
    if (command !== "go-prefix") clearGo();
    switch (command) {
      case "page-down":
      case "page-up": {
        noteUserScroll();
        const direction = command === "page-up" || event.shiftKey ? -1 : 1;
        const maxTop = Math.max(
          0,
          scroller.scrollHeight - scroller.clientHeight,
        );
        const top = Math.max(
          0,
          Math.min(
            maxTop,
            direction === 1
              ? nextGridPageTop(
                  rows(),
                  scroller.scrollTop,
                  scroller.clientHeight,
                )
              : scroller.scrollTop - scroller.clientHeight,
          ),
        );
        // Do not restart an animation against a boundary (including subpixel rounding).
        if (Math.abs(top - scroller.scrollTop) < 1) break;
        const viewport = scroller.getBoundingClientRect();
        const focusedRect = focusControl(
          props.focusedID,
        )?.getBoundingClientRect();
        pageFocus = {
          top,
          x: focusedRect
            ? (focusedRect.left + focusedRect.right) / 2 - viewport.left
            : viewport.width / 2,
          y: focusedRect
            ? Math.max(
                0,
                Math.min(
                  viewport.height,
                  (focusedRect.top + focusedRect.bottom) / 2 - viewport.top,
                ),
              )
            : viewport.height / 2,
        };
        scroller.scrollTo({
          behavior: window.matchMedia("(prefers-reduced-motion: reduce)")
            .matches
            ? "instant"
            : "smooth",
          top,
        });
        break;
      }
      case "down":
        move("down");
        break;
      case "up":
        move("up");
        break;
      case "left":
        move("left");
        break;
      case "right":
        move("right");
        break;
      case "open":
        if (item) openFocused(item);
        break;
      case "like": {
        const lead = focusedStory()?.items[0] ?? item;
        if (lead && !props.archive)
          props.onSignal(lead, lead.signal === 1 ? 0 : 1);
        break;
      }
      case "dislike": {
        const lead = focusedStory()?.items[0] ?? item;
        if (lead && !props.archive && !buryDisabled(lead))
          props.onSignal(lead, lead.signal === -1 ? 0 : -1);
        break;
      }
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
    <div
      class="grid-scroll"
      data-grid-images={imagesEnabled ? "on" : "off"}
      onPointerMove={() => setKeyboardFocus(false)}
      onPointerDown={() => {
        setKeyboardFocus(false);
        pageFocus = undefined;
      }}
      classList={{
        "refined-grid": true,
        "mobile-refined-grid": mobile(),
        "keyboard-focus": keyboardFocus(),
        "reader-underlay": props.readerOpen,
        "reader-underlay-dragging": props.readerDragging,
      }}
      style={{ "--reader-reveal": props.readerReveal }}
      ref={scroller}
      tabindex="-1"
    >
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
        <Show when={props.scopeCell} keyed>
          {(model) => (
            <h2 class="scope-cell" title={model.title}>
              <Show when={model.faviconFeed} keyed>
                {(feed) => (
                  <SourceBadge
                    connector={feed.connector}
                    imageURL={feed.favicon_url}
                    title={model.title}
                    size={mobile() ? 20 : 22}
                  />
                )}
              </Show>
              <span class="scope-cell__title">{model.title}</span>
              <span class="scope-cell__meta">
                <Show when={model.count !== undefined} fallback={model.text}>
                  <span class="scope-cell__count" data-zero={model.count === 0}>
                    {model.count?.toLocaleString("en-US")}
                  </span>
                  {model.text.slice(
                    model.count?.toLocaleString("en-US").length,
                  )}
                </Show>
              </span>
            </h2>
          )}
        </Show>
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
                  if (cell.story) {
                    const storyID = cell.story.story_id;
                    return (
                      <StoryCell
                        story={liveStories().get(storyID) ?? cell.story}
                        cell={cell}
                        row={row}
                        focusedID={props.focusedID}
                        readContext={readContext()}
                        refined={true}
                        pressed={pressedID() === `story:${storyID}`}
                        onExpand={(id) => props.onExpandStory?.(id)}
                        onLeadHeight={recordStoryLeadHeight}
                        onFocus={(id) => {
                          if (!pageFocus) props.onFocus(id);
                        }}
                        onOpenLead={openStoryLead}
                        onOpen={props.onOpen}
                        onExternalOpen={props.onExternalOpen}
                        onHeart={props.onHeart}
                        onSignal={props.onSignal}
                        onApplyFeed={props.onApplyFeed}
                        onMore={(story) => {
                          const lead = story.items[0];
                          if (lead) openSheet(lead, story);
                        }}
                        onLongPressStart={(event, story) => {
                          const lead = story.items[0];
                          if (lead) startLongPress(event, lead, story);
                        }}
                        onLongPressMove={moveLongPressGesture}
                        onLongPressEnd={cancelLongPress}
                      />
                    );
                  }
                  const item = createMemo(
                    () => liveItems().get(cell.item.item_id) ?? cell.item,
                  );
                  const signalFresh = createSignalFresh(() => item().signal);
                  const condensedLarge = createMemo(
                    () =>
                      width() < 700 &&
                      (row.kind === "hero" ||
                        row.kind === "pair" ||
                        row.kind === "tile"),
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
                        "is-read": readVisuals().dimmed,
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
                        "mobile-tile-cell": cell.mobileTile === true,
                        "sub-cell": row.kind === "span" && cell.span !== 2,
                        "compact-cell": row.kind === "compact",
                        [`size-${cell.effectiveSize.toLowerCase()}`]: true,
                        pressed: pressedID() === item().item_id,
                      }}
                      style={{
                        left: `${cell.left}px`,
                        top: `${cell.offsetY ?? 0}px`,
                        width: `${cell.width}px`,
                        height: `${cell.height ?? row.height}px`,
                      }}
                      data-signal={item().signal}
                      data-signal-fresh={signalFresh() ? "" : undefined}
                      data-item-id={item().item_id}
                      onMouseEnter={() => {
                        if (!pageFocus) props.onFocus(item().item_id);
                      }}
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
                          loading="eager"
                          deferUntilVisible
                          maxDimension={768}
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
                              row.kind === "hero" ||
                              row.kind === "pair" ||
                              row.kind === "tile"
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
                      <Show
                        when={
                          !props.archive &&
                          props.order === "interest" &&
                          !item().signal &&
                          whyText(item())
                        }
                      >
                        <span class="ranking-hint">{whyText(item())}</span>
                      </Show>
                      <div class="cell-scrim" />
                      <SignalMarker value={item().signal} />
                      <div class="cell-corner">
                        <SignalLabel value={item().signal} />
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
                          <span class="cell-main-parts">
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
                              aria-label={`Open ${headlineText(item().title)}${readVisuals().unreadDot ? ", unread" : ""}`}
                            />
                            <CellCopy
                              item={item()}
                              refined={true}
                              unreadDot={readVisuals().unreadDot}
                              archive={props.archive}
                              effectiveSize={cell.effectiveSize}
                              condensed={condensedLarge()}
                              explanation={explanation()}
                              dimmed={readVisuals().dimmed}
                              onUndo={() => props.onSignal(item(), 0)}
                              onApplyFeed={() => props.onApplyFeed(item())}
                            />
                          </span>
                        }
                      >
                        <span class="cell-main-parts">
                          <a
                            class="cell-main"
                            href={item().external_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            onClick={() => props.onExternalOpen(item())}
                            aria-label={`Open ${headlineText(item().title)} on ${externalHost(item().external_url)}${readVisuals().unreadDot ? ", unread" : ""}`}
                          >
                            <span class="sr-only">
                              Open {headlineText(item().title)}
                            </span>
                          </a>
                          <CellCopy
                            item={item()}
                            refined={true}
                            unreadDot={readVisuals().unreadDot}
                            archive={props.archive}
                            effectiveSize={cell.effectiveSize}
                            condensed={condensedLarge()}
                            explanation={explanation()}
                            dimmed={readVisuals().dimmed}
                            onUndo={() => props.onSignal(item(), 0)}
                            onApplyFeed={() => props.onApplyFeed(item())}
                          />
                        </span>
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
                      <SignalActions
                        item={item()}
                        size={cell.effectiveSize}
                        archive={props.archive}
                        onSignal={props.onSignal}
                        onHeart={props.onHeart}
                        onMore={() => openSheet(item())}
                      />
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
            classList={{
              "empty-grid": props.entries.length === 0,
              "finish-card":
                !props.archive && props.unreadOnly && props.entries.length > 0,
              "finish-card--all-read":
                !props.archive &&
                props.unreadOnly &&
                props.entries.length > 0 &&
                unreadIDs().length === 0,
              "closed-empty": Boolean(closedEmpty()),
              "closed-empty--centered": Boolean(closedEmpty()?.centered),
              "closed-empty--pending":
                Boolean(closedEmpty()?.centered) && props.pendingNewCount > 0,
              "no-action": !showEndAction(),
            }}
            style={{
              top: `${endTop()}px`,
              "min-height": closedEmpty()?.centered
                ? `${Math.max(viewportHeight(), width() < 520 ? 474 : 520)}px`
                : closedEmpty()
                  ? undefined
                  : `${viewportHeight()}px`,
            }}
          >
            <div>
              <Show
                when={!closedEmpty()}
                fallback={
                  <>
                    <i class="empty-mark" aria-hidden="true" />
                    <h2>{closedEmpty()?.heading}</h2>
                    <p>{closedEmpty()?.body}</p>
                    <div class="empty-actions">
                      <For each={closedEmpty()?.actions}>
                        {(action) => (
                          <button
                            type="button"
                            aria-keyshortcuts={
                              action.key === "Esc" ? "Escape" : action.key
                            }
                            onClick={action.run}
                          >
                            {action.label}
                            <Show when={action.key}>
                              <span class="empty-key" aria-hidden="true">
                                {action.key}
                              </span>
                            </Show>
                          </button>
                        )}
                      </For>
                    </div>
                  </>
                }
              >
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
                        when={props.entries.length > 0}
                        fallback={
                          <>
                            <i class="empty-mark" aria-hidden="true" />
                            <h2>You&apos;re all caught up</h2>
                            <p>
                              Unread is empty. Anything that arrives from here
                              on shows up at the top.
                            </p>
                          </>
                        }
                      >
                        <Show
                          when={showEndMarkAction()}
                          fallback={
                            <div class="finish-card__copy finish-card__variant">
                              <h2>
                                <i
                                  class="finish-card__mark"
                                  aria-hidden="true"
                                />
                                All caught up — everything here is already read
                              </h2>
                              <p>
                                Clearing marks nothing — it just empties the
                                grid.
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
                              {unreadIDs().length === 1
                                ? "item is"
                                : "items are"}{" "}
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
              <div
                class="sheet-scrim-visual"
                aria-hidden="true"
                style={{ opacity: sheetDrag.scrimOpacity() }}
              />
              <section
                ref={sheetPanel}
                class="action-sheet refined-sheet"
                classList={{ "sheet-dragging": sheetDrag.dragging() }}
                role="dialog"
                aria-modal="true"
                aria-label={`Actions for ${item.title}`}
                style={{ transform: `translateY(${sheetDrag.offset()}px)` }}
                onPointerDown={sheetDrag.onPointerDown}
                onPointerMove={sheetDrag.onPointerMove}
                onPointerUp={sheetDrag.onPointerUp}
                onPointerCancel={sheetDrag.onPointerCancel}
              >
                <i class="sheet-handle" aria-hidden="true" />
                <header>
                  <strong>{headlineText(item.title)}</strong>
                  <span>
                    <Show when={sheetStory()} keyed>
                      {(story) => (
                        <>
                          {story.source_count}{" "}
                          {story.source_count === 1 ? "source" : "sources"} ·{" "}
                        </>
                      )}
                    </Show>
                    {gridSourceName(item)} · {relativeTime(item.published_ts)}
                  </span>
                </header>
                <Show when={sheetStory()} keyed>
                  {(story) => {
                    const headlines = sheetHeadlineSlice(story);
                    return (
                      <Show when={story.items.length > 1}>
                        <div class="sheet-headlines">
                          <For each={headlines.items}>
                            {(headline) => (
                              <button
                                type="button"
                                class="sheet-headline"
                                classList={{
                                  read: readVisualState(
                                    readContext(),
                                    headline.read,
                                  ).dimmed,
                                  "related-also": repeatsLeadHeadline(
                                    story.items[0].title,
                                    headline.title,
                                  ),
                                }}
                                aria-label={`Open ${headlineText(headline.title)}`}
                                onClick={() =>
                                  runSheetAction(() => props.onOpen(headline))
                                }
                              >
                                <RelatedCoverage
                                  lead={story.items[0]}
                                  item={headline}
                                  age={relativeTime(headline.published_ts)}
                                />
                              </button>
                            )}
                          </For>
                          <Show when={headlines.remaining > 0}>
                            <button
                              type="button"
                              class="sheet-headline sheet-headline-more"
                              onClick={() =>
                                runSheetAction(() =>
                                  props.onOpenStoryLead?.(story),
                                )
                              }
                            >
                              +{headlines.remaining} more
                            </button>
                          </Show>
                        </div>
                      </Show>
                    );
                  }}
                </Show>
                <Show when={sheetStory() && !props.archive}>
                  <button
                    type="button"
                    onClick={() => {
                      const story = sheetStory();
                      if (story)
                        runSheetAction(() => props.onToggleStoryRead?.(story));
                    }}
                  >
                    <Icon name="check" size={20} />
                    {sheetStory()?.items.some((member) => !member.read)
                      ? "Mark read"
                      : "Mark unread"}
                  </button>
                </Show>
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
                      requestAnimationFrame(() =>
                        requestAnimationFrame(() =>
                          props.onSignal(item, item.signal === 1 ? 0 : 1),
                        ),
                      ),
                    )
                  }
                >
                  <Icon name="boost" size={20} />
                  Boost
                  <Show when={item.signal === 1}>
                    <span class="sheet-signal-on">on</span>
                  </Show>
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
                  disabled={buryDisabled(item)}
                  aria-label={
                    buryDisabled(item) ? "Kept items can't be buried" : "Bury"
                  }
                  onClick={() =>
                    runSheetAction(() =>
                      requestAnimationFrame(() =>
                        requestAnimationFrame(() => {
                          if (!buryDisabled(item))
                            props.onSignal(item, item.signal === -1 ? 0 : -1);
                        }),
                      ),
                    )
                  }
                >
                  <Icon name="bury" size={20} />
                  Bury
                  <Show when={item.signal === -1}>
                    <span class="sheet-signal-on">on</span>
                  </Show>
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

export function CellCopy(props: {
  item: Item;
  unreadDot: boolean;
  refined?: boolean;
  archive: boolean;
  effectiveSize: "S" | "M" | "L";
  condensed: boolean;
  explanation: string;
  dimmed?: boolean;
  onApplyFeed?(): void;
  story?: boolean;
  onUndo?(): void;
}) {
  const reddit = () => isRedditItem(props.item);
  const domain = () =>
    props.item.post_type === "link"
      ? externalHost(props.item.external_url)
      : "";
  return (
    <div class="cell-copy">
      <h2 classList={{ read: props.dimmed }}>
        {headlineText(props.item.title)}
      </h2>
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
        <Show when={props.refined && !props.archive}>
          <UnreadDot visible={props.unreadDot} />
        </Show>
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
        <Show
          when={!props.archive && props.onApplyFeed}
          fallback={
            <span>
              {props.refined
                ? gridSourceName(props.item)
                : props.item.feed_title || "Feed"}
              <Show when={reddit() && !props.refined}>
                {` · ${relativeTime(props.item.published_ts)}`}
              </Show>
            </span>
          }
        >
          <button
            type="button"
            class="cell-feed-filter"
            aria-label={`Filter by feed: ${props.refined ? gridSourceName(props.item) : props.item.feed_title || "Feed"}`}
            onPointerDown={(event) => event.stopPropagation()}
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              props.onApplyFeed?.();
            }}
          >
            {props.refined
              ? gridSourceName(props.item)
              : props.item.feed_title || "Feed"}
            <Show when={reddit() && !props.refined}>
              {` · ${relativeTime(props.item.published_ts)}`}
            </Show>
          </button>
        </Show>
        <Show when={props.refined}>
          <span class="refined-age">
            <span class="age-separator">· </span>
            {relativeTime(props.item.published_ts)}
          </span>
          <Show when={props.item.read && !props.archive}>
            <span class="refined-read-label">
              <Icon name="check" size={13} /> read
            </span>
          </Show>
        </Show>
      </div>
      <Show
        when={!props.archive && props.effectiveSize !== "S" && !props.dimmed}
      >
        <div
          class={`why-hint why-${props.effectiveSize.toLowerCase()}`}
          classList={{ "has-signal": props.item.signal !== 0 }}
        >
          <Show
            when={props.item.signal !== 0}
            fallback={
              props.effectiveSize === "L"
                ? props.explanation
                : whyText(props.item)
            }
          >
            <SignalWhy
              value={props.item.signal}
              story={props.story}
              onUndo={() => props.onUndo?.()}
            />
          </Show>
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
