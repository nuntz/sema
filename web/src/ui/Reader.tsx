import {
  createEffect,
  createMemo,
  createSignal,
  For,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import { Portal, render } from "solid-js/web";
import { isOlderThanThirtyDays } from "../archive";
import { useClock } from "../clock";
import { AppHeader } from "../components/AppHeader";
import { Icon } from "../components/Icon";
import { hoursLeft } from "../expiry";
import { decodeImageWithin } from "../image-decode";
import { createMediaQuery } from "../media-query";
import { readerDeadlineLine } from "../reader-expiry";
import { stripSummaryEcho } from "../reader-item";
import {
  externalHost,
  isRedditGallery,
  isRedditItem,
  type RedditReaderImageSource,
  redditReaderImageSources,
  redditReaderOriginalURL,
  redditSummaryProvenance,
  showsReaderOriginalFallback,
} from "../reddit-item";
import { signalActionLabel } from "../signal-feedback";
import type { Item } from "../types";
import { ExpiryPill } from "./ExpiryPill";
import { relativeTime } from "./Grid";
import { isEditingTarget, readerCommand } from "./keyboard";
import { Lightbox } from "./Lightbox";
import { buildLightboxSet, type LightboxImage } from "./lightbox-set";
import { closeOverlay, pushOverlay } from "./overlay-history";
import { ResponsiveImage } from "./ResponsiveImage";
import { type PreparedReaderBody, prepareReaderBody } from "./reader-content";
import { SourceBadge } from "./SourceBadge";
import {
  expandToolbar,
  initialToolbarCollapseState,
  updateToolbarCollapse,
} from "./toolbar-collapse";
import {
  beginSwipe,
  closeCommand,
  lockSwipeAxis,
  panelOffset,
  type SwipeGesture,
  swipeCommand,
  swipeOffset,
} from "./touch-gestures";
import { useSheetDrag } from "./use-sheet-drag";
import {
  type DescriptionToken,
  parseVideoDescription,
} from "./video-description";

interface ReaderProps {
  item: Item;
  active: boolean;
  archive: boolean;
  hearted: boolean;
  linkActionActive: boolean;
  canPrevious: boolean;
  canNext: boolean;
  closing: boolean;
  onClose(): void;
  onReveal(progress: number, dragging: boolean): void;
  onHome(): void;
  onPrevious(): void;
  onNext(): void;
  onSignal(value: -1 | 0 | 1): void;
  onHeart(): void;
  onCopy(): void;
  onOriginal(): void;
  onRelated(): void;
  onApplyFeed(): void;
  onRetry(): void;
  onDwell(itemID: string, dwellMS: number): void;
}

export function Reader(props: ReaderProps) {
  const now = useClock();
  const showLifetime = () =>
    !props.archive &&
    !props.item.archived &&
    !props.hearted &&
    !props.item.hearted;
  let article!: HTMLDivElement;
  let heading!: HTMLHeadingElement;
  let readerHeader!: HTMLElement;
  let toolbar!: HTMLElement;
  let moreButton!: HTMLButtonElement;
  let sheetPanel!: HTMLElement;
  let sheetFirstAction!: HTMLButtonElement;
  const [lightbox, setLightbox] = createSignal<{
    images: LightboxImage[];
    index: number;
  }>();
  const lightboxOpen = () => !!lightbox();
  const [redditImageAttempt, setRedditImageAttempt] =
    createSignal<RedditResolvedImageAttempt>();
  const [body, setBody] = createSignal<PreparedReaderBody>();
  const [loading, setLoading] = createSignal(
    !!props.item.body_url && props.item.has_body,
  );
  const [progress, setProgress] = createSignal(0);
  const [scrolled, setScrolled] = createSignal(false);
  const [headlineVisible, setHeadlineVisible] = createSignal(true);
  const [overflowOpen, setOverflowOpen] = createSignal(false);
  const [sheetOpen, setSheetOpen] = createSignal(false);
  const [toolbarState, setToolbarState] = createSignal(
    initialToolbarCollapseState(),
  );
  const [dragOffset, setDragOffset] = createSignal(0);
  const [panelX, setPanelX] = createSignal(0);
  const [swiping, setSwiping] = createSignal(false);
  // Judgment updates replace the item object without changing article content.
  const bodySource = createMemo(
    () => ({
      itemID: props.item.item_id,
      url: props.item.body_url,
      hasBody: props.item.has_body,
    }),
    undefined,
    {
      equals: (previous, next) =>
        previous?.itemID === next.itemID &&
        previous?.url === next.url &&
        previous?.hasBody === next.hasBody,
    },
  );
  const narrowHeader = createMediaQuery("(max-width: 619px)");
  const mediumHeader = createMediaQuery(
    "(min-width: 620px) and (max-width: 1199px)",
  );
  const showExpiryPill = () =>
    showLifetime() && hoursLeft(props.item.published_ts, now()) <= 48;
  const lifetimeOverflow = () => showExpiryPill() && mediumHeader();
  let trackedID = props.item.item_id;
  let dwellMS = 0;
  let activeSince = 0;
  let lastReported = 0;
  let thresholdReported = false;
  let dwellTimer: number | undefined;
  let carryTimer: number | undefined;
  let carryFrame = 0;
  let sheetFocusFrame = 0;
  let toolbarTapTimer: number | undefined;
  let suppressToolbarAction = false;
  let carryingNext = false;
  let swipe: SwipeGesture | undefined;
  let touchX = 0;

  const displaySummary = createMemo(() =>
    stripSummaryEcho(props.item.summary ?? "", props.item.title),
  );
  const headerScrolled = createMemo(() =>
    narrowHeader() ? !headlineVisible() : scrolled(),
  );

  const originalReason = (): "extraction" | "titles-only" =>
    !displaySummary() && props.item.extract_quality === 0
      ? "titles-only"
      : "extraction";

  const startDwell = () => {
    if (activeSince || document.visibilityState === "hidden") return;
    activeSince = performance.now();
  };

  const pauseDwell = () => {
    if (!activeSince) return;
    dwellMS += performance.now() - activeSince;
    activeSince = 0;
  };

  const currentDwell = () =>
    dwellMS + (activeSince ? performance.now() - activeSince : 0);

  const reportDwell = () => {
    const elapsed = Math.round(currentDwell());
    if (elapsed <= lastReported) return;
    lastReported = elapsed;
    props.onDwell(trackedID, elapsed);
  };

  createEffect(() => {
    const itemID = props.item.item_id;
    if (itemID === trackedID) return;
    pauseDwell();
    reportDwell();
    trackedID = itemID;
    dwellMS = 0;
    lastReported = 0;
    thresholdReported = false;
    setProgress(0);
    setScrolled(false);
    setHeadlineVisible(true);
    setOverflowOpen(false);
    setToolbarState(initialToolbarCollapseState());
    if (sheetOpen()) {
      closeOverlay("action-sheet");
      setSheetOpen(false);
    }
    if (!carryingNext) setDragOffset(0);
    if (article) article.scrollTop = 0;
    startDwell();
  });

  createEffect(() => {
    const source = bodySource();
    const url = source.url;
    setBody(undefined);
    setLoading(false);
    if (!url || !source.hasBody) return;
    const controller = new AbortController();
    setLoading(true);
    fetch(url, { credentials: "same-origin", signal: controller.signal })
      .then((response) => {
        if (!response.ok) throw new Error("body unavailable");
        return response.text();
      })
      .then(async (markup) => {
        if (controller.signal.aborted) return;
        const prepared = markup.trim()
          ? await prepareReaderBody(markup)
          : undefined;
        if (!controller.signal.aborted) setBody(prepared);
      })
      .catch((error) => {
        if (!controller.signal.aborted && error.name !== "AbortError")
          setBody(undefined);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    onCleanup(() => controller.abort());
  });

  const focusMoreButton = () => {
    cancelAnimationFrame(sheetFocusFrame);
    sheetFocusFrame = requestAnimationFrame(() =>
      moreButton?.focus({ preventScroll: true }),
    );
  };

  const hideSheet = () => {
    setSheetOpen(false);
    focusMoreButton();
  };

  const openSheet = () => {
    if (sheetOpen()) return;
    pushOverlay("action-sheet", hideSheet);
    setSheetOpen(true);
    cancelAnimationFrame(sheetFocusFrame);
    sheetFocusFrame = requestAnimationFrame(() =>
      sheetFirstAction?.focus({ preventScroll: true }),
    );
  };

  const closeSheet = () => {
    if (!sheetOpen()) return;
    closeOverlay("action-sheet");
    hideSheet();
  };

  const runSheetAction = (action: () => void) => {
    closeSheet();
    action();
  };

  const sheetDrag = useSheetDrag({
    panel: () => sheetPanel,
    onDismiss: closeSheet,
  });

  const revealToolbar = () => setToolbarState((state) => expandToolbar(state));

  const onToolbarPointerDown = (event: PointerEvent) => {
    if (!toolbarState().collapsed) return;
    suppressToolbarAction = true;
    event.preventDefault();
    revealToolbar();
  };

  const onToolbarPointerUp = (event: PointerEvent) => {
    if (!suppressToolbarAction) return;
    event.preventDefault();
    window.clearTimeout(toolbarTapTimer);
    toolbarTapTimer = window.setTimeout(() => {
      suppressToolbarAction = false;
    }, 0);
  };

  const onToolbarPointerCancel = () => {
    suppressToolbarAction = false;
    window.clearTimeout(toolbarTapTimer);
  };

  const onToolbarClick = (event: MouseEvent) => {
    if (!suppressToolbarAction) return;
    suppressToolbarAction = false;
    event.preventDefault();
    event.stopPropagation();
  };

  createEffect(() => {
    if (!narrowHeader()) {
      setHeadlineVisible(true);
      setToolbarState((state) => expandToolbar(state));
      if (sheetOpen()) closeSheet();
      return;
    }
    if (typeof IntersectionObserver === "undefined") return;

    const navHeight = Math.ceil(readerHeader.getBoundingClientRect().height);
    const observer = new IntersectionObserver(
      ([entry]) => setHeadlineVisible(entry?.isIntersecting ?? true),
      {
        root: article,
        rootMargin: `-${navHeight}px 0px 0px 0px`,
        threshold: 0,
      },
    );
    observer.observe(heading);
    onCleanup(() => observer.disconnect());
  });

  const updateProgress = () => {
    const range = article.scrollHeight - article.clientHeight;
    setProgress(range <= 0 ? 1 : article.scrollTop / range);
    if (narrowHeader()) {
      setToolbarState((state) =>
        updateToolbarCollapse(
          state,
          article.scrollTop,
          range <= 0 || article.scrollTop >= range - 1,
        ),
      );
    } else {
      setScrolled((current) =>
        current ? article.scrollTop > 160 : article.scrollTop >= 180,
      );
    }
  };

  const onTouchStart = (event: TouchEvent) => {
    if (lightboxOpen() || event.touches.length !== 1) return;
    const touch = event.touches[0];
    touchX = touch.clientX;
    swipe = beginSwipe(
      touch.clientX,
      touch.clientY,
      performance.now(),
      window.innerWidth,
      window.matchMedia("(display-mode: standalone)").matches ||
        (navigator as Navigator & { standalone?: boolean }).standalone === true,
    );
  };

  const onTouchMove = (event: TouchEvent) => {
    if (!swipe || event.touches.length !== 1) return;
    const touch = event.touches[0];
    touchX = touch.clientX;
    const axis = lockSwipeAxis(swipe, touch.clientX, touch.clientY);
    if (axis !== "horizontal") return;
    event.preventDefault();
    setSwiping(true);
    const offset = panelOffset(swipe, touch.clientX, window.innerWidth);
    setPanelX(offset);
    setDragOffset(
      offset > 0 ? 0 : swipeOffset(swipe, touch.clientX, props.canNext),
    );
    props.onReveal(offset / window.innerWidth, true);
  };

  const carryToNext = () => {
    carryingNext = true;
    setDragOffset(-window.innerWidth);
    const duration = window.matchMedia("(prefers-reduced-motion: reduce)")
      .matches
      ? 0
      : 200;
    carryTimer = window.setTimeout(() => {
      setSwiping(true);
      setDragOffset(window.innerWidth);
      props.onNext();
      carryFrame = requestAnimationFrame(() => {
        carryFrame = requestAnimationFrame(() => {
          carryingNext = false;
          setSwiping(false);
          setDragOffset(0);
        });
      });
    }, duration);
  };

  const finishSwipe = () => {
    if (!swipe) return;
    const endedAt = performance.now();
    const close = closeCommand(swipe, touchX, endedAt, window.innerWidth);
    const command = swipeCommand(swipe, touchX, endedAt);
    swipe = undefined;
    setSwiping(false);
    if (close) {
      setPanelX(window.innerWidth);
      props.onReveal(1, false);
      props.onClose();
      return;
    }
    setPanelX(0);
    props.onReveal(0, false);
    if (command === "next" && props.canNext) carryToNext();
    else setDragOffset(0);
  };

  const cancelSwipe = () => {
    swipe = undefined;
    setSwiping(false);
    setDragOffset(0);
    setPanelX(0);
    props.onReveal(0, false);
  };

  createEffect(() => {
    body();
    redditImageAttempt();
    const item = props.item;
    let disposed = false;
    let removeAffordances = () => {};
    let decorated: HTMLImageElement[] = [];
    const rebuild = () => {
      if (disposed) return;
      const lead =
        item.media_type !== "video"
          ? article.querySelector<HTMLImageElement>(
              isRedditItem(item)
                ? ".lb-lead-host img.reddit-lead-image"
                : ".article-lead",
            )
          : null;
      const images = buildLightboxSet(
        article.querySelector(".article-body"),
        lead
          ? {
              element: lead,
              src: new URL(item.media_url || lead.src, document.baseURI).href,
              alt: lead.alt,
              caption: "",
              width: item.media_w,
              height: item.media_h,
              variants: item.media_variants ?? [],
            }
          : undefined,
      );
      if (
        images.length === decorated.length &&
        images.every((member, index) => member.element === decorated[index])
      )
        return;
      removeAffordances();
      decorated = images.map((member) => member.element);
      const cleanups = images.map((member, index) => {
        const element = member.element;
        const previousTab = element.getAttribute("tabindex");
        // The lead remains in its Solid-owned parent; only injected body nodes move.
        const leadHost = element.closest<HTMLElement>(".lb-lead-host");
        const wrapper = leadHost ?? document.createElement("span");
        wrapper.classList.add("lb-inline");
        if (!leadHost) {
          element.before(wrapper);
          wrapper.append(element);
        }
        const pill = document.createElement("span");
        pill.className = "lb-hover-pill";
        pill.setAttribute("aria-hidden", "true");
        const disposePill = render(
          () => <Icon name="expand" size={13} />,
          pill,
        );
        if (images.length > 1) pill.append(` ${index + 1} / ${images.length}`);
        wrapper.append(pill);
        element.classList.add("lb-openable");
        element.tabIndex = 0;
        const open = () => setLightbox({ images, index });
        const click = (event: MouseEvent) => {
          if (
            event.button !== 0 ||
            event.ctrlKey ||
            event.metaKey ||
            event.shiftKey ||
            event.altKey
          )
            return;
          event.preventDefault();
          event.stopPropagation();
          open();
        };
        const key = (event: KeyboardEvent) => {
          if (event.key !== "Enter" && event.key !== " ") return;
          event.preventDefault();
          open();
        };
        wrapper.addEventListener("click", click);
        element.addEventListener("keydown", key);
        return () => {
          disposePill();
          wrapper.removeEventListener("click", click);
          element.removeEventListener("keydown", key);
          element.classList.remove("lb-openable");
          if (previousTab === null) element.removeAttribute("tabindex");
          else element.setAttribute("tabindex", previousTab);
          pill.remove();
          if (leadHost) wrapper.classList.remove("lb-inline");
          else if (wrapper.parentNode) wrapper.replaceWith(element);
        };
      });
      removeAffordances = () =>
        cleanups.forEach((cleanup) => {
          cleanup();
        });
    };
    queueMicrotask(rebuild);
    // Extracted images may have no dimensions until their first inline load.
    article.addEventListener("load", rebuild, true);
    onCleanup(() => {
      article.removeEventListener("load", rebuild, true);
      disposed = true;
      removeAffordances();
      setLightbox(undefined);
    });
  });

  const onKey = (event: KeyboardEvent) => {
    if (
      event.defaultPrevented ||
      event.isComposing ||
      isEditingTarget(event.target)
    )
      return;
    if (!props.active || lightboxOpen()) return;
    if (sheetOpen() && event.key === "Escape") {
      closeSheet();
      event.preventDefault();
      return;
    }
    if (sheetOpen() || event.metaKey || event.ctrlKey || event.altKey) return;
    const command = readerCommand(event.key);
    const target = event.target;
    if (
      command === "page-down" &&
      event.key === " " &&
      target instanceof HTMLElement &&
      (target.isContentEditable ||
        target.matches("button, input, select, textarea, summary"))
    )
      return;
    switch (command) {
      case "close":
        props.onClose();
        break;
      case "next":
        if (props.canNext) props.onNext();
        break;
      case "previous":
        if (props.canPrevious) props.onPrevious();
        break;
      case "page-down":
      case "page-up": {
        const direction = command === "page-up" || event.shiftKey ? -1 : 1;
        const maxTop = Math.max(0, article.scrollHeight - article.clientHeight);
        const top = Math.max(
          0,
          Math.min(
            maxTop,
            article.scrollTop + article.clientHeight * direction,
          ),
        );
        // Do not restart an animation against a boundary (including subpixel rounding).
        if (Math.abs(top - article.scrollTop) < 1) break;
        article.scrollTo({
          behavior: window.matchMedia("(prefers-reduced-motion: reduce)")
            .matches
            ? "instant"
            : "smooth",
          top,
        });
        break;
      }
      case "like":
        props.onSignal(props.item.signal === 1 ? 0 : 1);
        break;
      case "dislike":
        if (!props.hearted) props.onSignal(props.item.signal === -1 ? 0 : -1);
        break;
      case "heart":
        props.onHeart();
        break;
      case "copy":
        props.onCopy();
        break;
      case "original":
        props.onOriginal();
        window.open(props.item.url, "_blank", "noopener,noreferrer");
        break;
      case "related":
        props.onRelated();
        break;
      default:
        return;
    }
    event.preventDefault();
  };

  onMount(() => {
    const pauseAndReport = () => {
      pauseDwell();
      reportDwell();
    };
    const onVisibility = () => {
      if (document.visibilityState === "hidden") pauseAndReport();
      else startDwell();
    };
    const onFocus = () => startDwell();
    const onBlur = () => pauseAndReport();
    startDwell();
    dwellTimer = window.setInterval(() => {
      if (!thresholdReported && currentDwell() >= 30_000) {
        thresholdReported = true;
        reportDwell();
      }
    }, 1_000);
    window.addEventListener("keydown", onKey);
    window.addEventListener("focus", onFocus);
    window.addEventListener("blur", onBlur);
    document.addEventListener("visibilitychange", onVisibility);
    article.addEventListener("scroll", updateProgress, { passive: true });
    article.addEventListener("touchstart", onTouchStart, { passive: true });
    article.addEventListener("touchmove", onTouchMove, { passive: false });
    article.addEventListener("touchend", finishSwipe, { passive: true });
    article.addEventListener("touchcancel", cancelSwipe, { passive: true });
    toolbar.addEventListener("click", onToolbarClick, true);
    onCleanup(() => {
      pauseAndReport();
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("focus", onFocus);
      window.removeEventListener("blur", onBlur);
      document.removeEventListener("visibilitychange", onVisibility);
      article.removeEventListener("scroll", updateProgress);
      article.removeEventListener("touchstart", onTouchStart);
      article.removeEventListener("touchmove", onTouchMove);
      article.removeEventListener("touchend", finishSwipe);
      article.removeEventListener("touchcancel", cancelSwipe);
      toolbar.removeEventListener("click", onToolbarClick, true);
      window.clearInterval(dwellTimer);
      window.clearTimeout(carryTimer);
      window.clearTimeout(toolbarTapTimer);
      cancelAnimationFrame(carryFrame);
      cancelAnimationFrame(sheetFocusFrame);
      if (sheetOpen()) closeOverlay("action-sheet");
    });
  });

  return (
    <section
      class="reader"
      classList={{ closing: props.closing, swiping: swiping() }}
      style={{ "--reader-panel-x": `${panelX()}px` }}
      role="dialog"
      aria-modal="true"
      aria-label={props.item.title}
    >
      <AppHeader
        ref={(element) => {
          readerHeader = element;
        }}
        view="reader"
        onHome={props.onHome}
        scrolled={headerScrolled()}
        progress={progress()}
      >
        <button
          type="button"
          class="reader-back"
          onClick={props.onClose}
          aria-label="Back to grid"
        >
          <Icon name={narrowHeader() ? "previous-item" : "back-to-grid"} />
        </button>
        <div class="reader-slot">
          <SourceBadge
            connector={props.item.connector}
            imageURL={props.item.favicon_url}
            title={props.item.feed_title}
            size={20}
            class="reader-favicon reader-badge"
          />
          <span class="reader-slot__text">
            <span class="reader-crumb" aria-hidden={headerScrolled()}>
              <span class="reader-crumb__identity">
                <Show
                  when={!narrowHeader() && !props.archive}
                  fallback={
                    <span class="reader-crumb__source">
                      {props.item.feed_title || "Feed"}
                    </span>
                  }
                >
                  <button
                    type="button"
                    class="reader-crumb__source reader-feed-filter"
                    aria-label={`Filter by feed: ${props.item.feed_title || "Feed"}`}
                    tabIndex={headerScrolled() ? -1 : 0}
                    onClick={(event) => {
                      event.preventDefault();
                      event.stopPropagation();
                      props.onApplyFeed();
                    }}
                  >
                    {props.item.feed_title || "Feed"}
                  </button>
                </Show>
                <Show when={!showExpiryPill()}>
                  <span class="reader-crumb__meta">
                    {" "}
                    ·{" "}
                    {relativeTime(
                      props.item.display_date || props.item.published_ts,
                    )}{" "}
                    ago
                  </span>
                </Show>
              </span>
              <Show when={showExpiryPill()}>
                <span class="reader-life">
                  <ExpiryPill
                    labelled
                    published={props.item.published_ts}
                    now={now()}
                  />
                </span>
              </Show>
            </span>
            <span class="reader-title" aria-hidden={!headerScrolled()}>
              {props.item.title}
            </span>
          </span>
        </div>
        <div class="chrome-group chrome-group--judge">
          <button
            type="button"
            class="chrome-btn chrome-btn--quiet chrome-btn--collapse-2"
            classList={{ "chrome-btn--on": props.item.signal === 1 }}
            aria-label={signalActionLabel(
              props.item.signal,
              "boost",
              props.hearted,
            )}
            data-action="boost"
            aria-pressed={props.item.signal === 1}
            onClick={() => props.onSignal(props.item.signal === 1 ? 0 : 1)}
          >
            <Icon name="boost" />
            <span class="chrome-btn__label">
              {props.item.signal === 1 ? "boosted" : "boost"}
            </span>
          </button>
          <button
            type="button"
            class="chrome-btn chrome-btn--quiet chrome-btn--collapse-2"
            classList={{ "chrome-btn--on": props.item.signal === -1 }}
            aria-label={signalActionLabel(
              props.item.signal,
              "bury",
              props.hearted,
            )}
            data-action="bury"
            disabled={props.hearted}
            aria-pressed={props.item.signal === -1}
            onClick={() => props.onSignal(props.item.signal === -1 ? 0 : -1)}
          >
            <Icon name="bury" />
            <span class="chrome-btn__label">
              {props.item.signal === -1 ? "buried" : "bury"}
            </span>
          </button>
          <button
            type="button"
            class="chrome-btn chrome-btn--hold"
            classList={{ "chrome-btn--on": props.hearted }}
            aria-pressed={props.hearted}
            onClick={props.onHeart}
          >
            <Icon name="keep" filled={props.hearted} />
            <span class="chrome-btn__label">
              {props.hearted ? "kept" : "keep"}
            </span>
          </button>
        </div>
        <div class="chrome-group chrome-group--secondary">
          <button
            type="button"
            class="chrome-btn chrome-btn--quiet chrome-btn--collapse-1"
            classList={{ "chrome-btn--on": props.linkActionActive }}
            onClick={props.onCopy}
          >
            {props.linkActionActive ? (
              <Icon name="check" />
            ) : (
              <Icon name="copy-link" />
            )}
            <span class="chrome-btn__label">copy link</span>
          </button>
          <button
            type="button"
            class="chrome-btn chrome-btn--quiet chrome-btn--collapse-1"
            onClick={props.onRelated}
          >
            <Icon name="search" />
            <span class="chrome-btn__label">similar</span>
          </button>
        </div>
        <span class="chrome-divider reader-leave-divider" aria-hidden="true" />
        <a
          href={props.item.url}
          target="_blank"
          rel="noopener noreferrer"
          class="chrome-btn chrome-btn--quiet chrome-btn--original chrome-btn--collapse-1"
          onClick={props.onOriginal}
        >
          <span class="chrome-btn__label">
            {isRedditItem(props.item) ? "discussion" : "original"}
          </span>
          <Icon name="open-original" />
        </a>
        <button
          type="button"
          class="chrome-btn chrome-btn--icon chrome-overflow"
          classList={{ "is-hidden": !narrowHeader() && !lifetimeOverflow() }}
          aria-label="More actions"
          aria-haspopup="menu"
          aria-expanded={overflowOpen()}
          onClick={() => setOverflowOpen((open) => !open)}
        >
          <Icon name="more" />
        </button>
        <div class="chrome-group chrome-group--page">
          <button
            type="button"
            class="chrome-btn chrome-btn--icon chrome-btn--prev"
            onClick={props.onPrevious}
            disabled={!props.canPrevious}
            aria-label="Previous item"
          >
            <Icon name="previous-item" />
            <span class="chrome-btn__label">prev</span>
          </button>
          <button
            type="button"
            class="chrome-btn chrome-btn--emphasis"
            onClick={props.onNext}
            disabled={!props.canNext}
            aria-label="Next unread item"
          >
            <span>next</span>
            <Icon name="next-item" />
          </button>
        </div>
        <Show when={overflowOpen() && (narrowHeader() || lifetimeOverflow())}>
          <div class="reader-overflow-menu" role="menu">
            <button
              type="button"
              class="chrome-btn"
              role="menuitem"
              onClick={() => {
                setOverflowOpen(false);
                props.onCopy();
              }}
            >
              {props.linkActionActive ? (
                <Icon name="check" />
              ) : (
                <Icon name="copy-link" />
              )}
              <span>copy link</span>
            </button>
            <button
              type="button"
              class="chrome-btn"
              role="menuitem"
              onClick={() => {
                setOverflowOpen(false);
                props.onRelated();
              }}
            >
              <Icon name="search" />
              <span>similar</span>
            </button>
            <a
              href={props.item.url}
              target="_blank"
              rel="noopener noreferrer"
              class="chrome-btn"
              role="menuitem"
              onClick={() => {
                setOverflowOpen(false);
                props.onOriginal();
              }}
            >
              <span>
                {isRedditItem(props.item) ? "discussion" : "original"}
              </span>
              <Icon name="open-original" />
            </a>
          </div>
        </Show>
      </AppHeader>
      <div
        class="reader-scroll"
        classList={{ swiping: swiping() }}
        style={{ transform: `translate3d(${dragOffset()}px, 0, 0)` }}
        ref={article}
      >
        <article class="article">
          <Show
            when={
              props.item.media_type !== "video" && !isRedditItem(props.item)
            }
          >
            <div class="article-kicker">
              ARTICLE ·{" "}
              {Math.max(
                1,
                Math.round(
                  (body()?.markup || displaySummary() || "").split(/\s+/)
                    .length / 220,
                ),
              )}{" "}
              MIN READ
            </div>
          </Show>
          <h1 ref={heading}>{props.item.title}</h1>
          <Show when={showLifetime()}>
            <p
              class="reader-deadline"
              classList={{
                "reader-deadline--urgent":
                  hoursLeft(props.item.published_ts, now()) < 6,
              }}
            >
              {readerDeadlineLine(props.item.published_ts, now())}
            </p>
          </Show>
          <Show when={!isRedditItem(props.item)}>
            <Show
              when={props.item.media_type === "video"}
              fallback={
                <p class="byline">
                  {props.item.author
                    ? `By ${props.item.author}`
                    : props.item.feed_title}
                  <Show when={props.item.display_date}>
                    {` · ${new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(new Date(props.item.display_date ?? ""))}`}
                  </Show>
                </p>
              }
            >
              <VideoMediaCard item={props.item} onOriginal={props.onOriginal} />
              <div class="video-channel-line">
                <SourceBadge
                  connector={props.item.connector}
                  imageURL={props.item.favicon_url}
                  title={props.item.feed_title}
                  size={28}
                />
                <strong>{props.item.feed_title || props.item.author}</strong>
                <span>
                  · published{" "}
                  {new Intl.DateTimeFormat(undefined, {
                    day: "numeric",
                    month: "short",
                    year: "numeric",
                  }).format(
                    new Date(
                      props.item.display_date || props.item.published_ts,
                    ),
                  )}
                </span>
              </div>
            </Show>
          </Show>
          <Show
            when={isRedditItem(props.item) ? props.item.item_id : undefined}
            keyed
          >
            {(_itemID) => (
              <RedditReaderIntro
                item={props.item}
                onClickThrough={props.onOriginal}
                onImageChange={setRedditImageAttempt}
              />
            )}
          </Show>
          <Show when={displaySummary()} keyed>
            {(summary) => (
              <div class="article-summary">
                <Show when={props.item.summary_source === "generated"}>
                  <div class="summary-provenance">
                    {isRedditItem(props.item)
                      ? redditSummaryProvenance(props.item)
                      : "summary · generated"}
                  </div>
                </Show>
                <p>{summary}</p>
              </div>
            )}
          </Show>
          <Show
            when={
              props.item.media_type !== "video" &&
              !isRedditItem(props.item) &&
              props.item.media_url &&
              !loading() &&
              !body()?.leadingImage &&
              props.item.item_id
            }
            keyed
          >
            {(_itemID) => (
              <div class="lb-lead-host">
                <ResponsiveImage
                  class="article-lead"
                  style={{
                    "--article-lead-width":
                      props.item.media_w && props.item.media_h
                        ? `min(${props.item.media_w}px, calc(var(--article-lead-max-height) * ${props.item.media_w / props.item.media_h}))`
                        : undefined,
                  }}
                  item={props.item}
                  sizes="(max-width: 700px) calc(100vw - 44px), 640px"
                  alt=""
                  width={props.item.media_w}
                  height={props.item.media_h}
                />
              </div>
            )}
          </Show>
          <Show when={props.item.media_type === "video"}>
            <VideoDescription
              description={props.item.description || ""}
              videoURL={props.item.url}
              onOriginal={props.onOriginal}
            />
          </Show>
          <Show
            when={props.item.media_type !== "video" && body()}
            fallback={
              <Show
                when={props.item.media_type === "video" || !loading()}
                fallback={
                  <p class="extraction-loading">
                    Loading the extracted article…
                  </p>
                }
              >
                <Show
                  when={
                    props.item.media_type !== "video" &&
                    showsReaderOriginalFallback(props.item)
                  }
                >
                  <OriginalRequired
                    reason={originalReason()}
                    url={redditReaderOriginalURL(props.item)}
                    retry={
                      !props.archive && originalReason() === "extraction"
                        ? props.onRetry
                        : undefined
                    }
                    onOriginal={props.onOriginal}
                  />
                </Show>
              </Show>
            }
          >
            {body()?.element}
          </Show>
          <Show
            when={
              props.archive &&
              props.item.media_type !== "video" &&
              !isRedditItem(props.item)
            }
          >
            <div class="archive-original">
              <Show when={isOlderThanThirtyDays(props.item.published_ts)}>
                <p class="archive-stale-note">
                  <b>!</b>
                  <span>
                    Published over 30 days ago; the original may have moved.
                  </span>
                </p>
              </Show>
              <a
                class="original-cta"
                href={props.item.url}
                target="_blank"
                rel="noopener noreferrer"
                onClick={props.onOriginal}
              >
                Open original
                <Icon name="open-original" />
              </a>
            </div>
          </Show>
          <footer class="article-next">
            <span>{props.archive ? "NEXT KEPT" : "NEXT UNREAD"}</span>
            <button
              type="button"
              onClick={props.onNext}
              disabled={!props.canNext}
            >
              <span>
                {props.canNext
                  ? "Continue to the next item"
                  : "You’re at the end"}
              </span>
              <Show when={props.canNext}>
                <Icon name="next-item" />
              </Show>
            </button>
          </footer>
        </article>
      </div>
      <nav
        ref={toolbar}
        class="reader-bottom-actions"
        classList={{ collapsed: toolbarState().collapsed }}
        aria-label="Article actions"
        onPointerDown={onToolbarPointerDown}
        onPointerUp={onToolbarPointerUp}
        onPointerCancel={onToolbarPointerCancel}
        onFocusIn={revealToolbar}
      >
        <div class="reader-bottom-actions__row">
          <button
            type="button"
            class="reader-toolbar-action"
            aria-label={signalActionLabel(
              props.item.signal,
              "boost",
              props.hearted,
            )}
            data-action="boost"
            aria-pressed={props.item.signal === 1}
            onClick={() => props.onSignal(props.item.signal === 1 ? 0 : 1)}
          >
            <Icon name="boost" />
          </button>
          <button
            type="button"
            class="reader-toolbar-action"
            aria-label={signalActionLabel(
              props.item.signal,
              "bury",
              props.hearted,
            )}
            data-action="bury"
            disabled={props.hearted}
            aria-pressed={props.item.signal === -1}
            onClick={() => props.onSignal(props.item.signal === -1 ? 0 : -1)}
          >
            <Icon name="bury" />
          </button>
          <button
            type="button"
            class="reader-toolbar-action"
            aria-label={
              props.hearted ? "Remove from archive" : "Keep in archive"
            }
            aria-pressed={props.hearted}
            onClick={props.onHeart}
          >
            <Icon name="keep" filled={props.hearted} />
          </button>
          <button
            ref={moreButton}
            type="button"
            class="reader-toolbar-action"
            aria-label="More actions"
            aria-haspopup="dialog"
            aria-expanded={sheetOpen()}
            onClick={openSheet}
          >
            <Icon name="more" />
          </button>
          <span class="reader-bottom-actions__gap" aria-hidden="true" />
          <button
            type="button"
            class="reader-toolbar-action"
            onClick={props.onPrevious}
            disabled={!props.canPrevious}
            aria-label="Previous item"
          >
            <Icon name="previous-item" />
          </button>
          <button
            type="button"
            class="reader-toolbar-action"
            onClick={props.onNext}
            disabled={!props.canNext}
            aria-label="Next unread item"
          >
            <Icon name="next-item" />
          </button>
        </div>
      </nav>
      <Show when={lightbox()}>
        {(state) => (
          <Lightbox
            images={state().images}
            initialIndex={state().index}
            onClose={() => setLightbox(undefined)}
          />
        )}
      </Show>
      <Portal>
        <Show when={sheetOpen()}>
          <div
            class="action-sheet-layer reader-action-sheet-layer"
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
              class="action-sheet reader-action-sheet"
              classList={{ "sheet-dragging": sheetDrag.dragging() }}
              role="dialog"
              aria-modal="true"
              aria-label={`Actions for ${props.item.title}`}
              style={{ transform: `translateY(${sheetDrag.offset()}px)` }}
              onPointerDown={sheetDrag.onPointerDown}
              onPointerMove={sheetDrag.onPointerMove}
              onPointerUp={sheetDrag.onPointerUp}
              onPointerCancel={sheetDrag.onPointerCancel}
            >
              <i class="sheet-handle" aria-hidden="true" />
              <header>
                <strong>{props.item.title}</strong>
                <span>{props.item.feed_title || "Feed"}</span>
              </header>
              <button
                ref={sheetFirstAction}
                type="button"
                onClick={() => runSheetAction(props.onCopy)}
              >
                <Icon
                  name={props.linkActionActive ? "check" : "copy-link"}
                  size={20}
                />
                Copy link
              </button>
              <button
                type="button"
                onClick={() => runSheetAction(props.onRelated)}
              >
                <Icon name="search" size={20} />
                Similar
              </button>
              <button
                type="button"
                onClick={() =>
                  runSheetAction(() => {
                    props.onOriginal();
                    window.open(
                      props.item.url,
                      "_blank",
                      "noopener,noreferrer",
                    );
                  })
                }
              >
                <Icon name="open-original" size={20} />
                {isRedditItem(props.item) ? "Discussion" : "Original"}
              </button>
            </section>
          </div>
        </Show>
      </Portal>
    </section>
  );
}

function RedditReaderIntro(props: {
  item: Item;
  onClickThrough(): void;
  onImageChange(attempt: RedditResolvedImageAttempt | undefined): void;
}) {
  const destination = () => props.item.external_url || "";
  const textPost = () => props.item.post_type === "text";
  const imagePost = () =>
    props.item.post_type === "image" || isRedditGallery(props.item);
  return (
    <>
      <Show when={textPost()}>
        <RedditSourceLine item={props.item} />
      </Show>
      <Show
        when={imagePost()}
        fallback={
          <Show when={destination()}>
            <RedditDestinationCard
              item={props.item}
              onClickThrough={props.onClickThrough}
            />
          </Show>
        }
      >
        <RedditImageCard
          onImageChange={props.onImageChange}
          item={props.item}
          onClickThrough={props.onClickThrough}
        />
      </Show>
      <div class="reddit-reader-actions">
        <a
          href={props.item.url}
          target="_blank"
          rel="noopener noreferrer"
          onClick={props.onClickThrough}
        >
          <Icon name="discussion" size={14} />
          Discussion
        </a>
      </div>
      <Show when={!textPost()}>
        <RedditSourceLine item={props.item} />
      </Show>
    </>
  );
}

function RedditDestinationCard(props: { item: Item; onClickThrough(): void }) {
  return (
    <a
      class="reddit-media-card"
      href={props.item.external_url}
      target="_blank"
      rel="noopener noreferrer"
      onClick={props.onClickThrough}
    >
      <Show when={props.item.media_url}>
        <span class="reddit-media-band">
          <ResponsiveImage
            item={props.item}
            sizes="(max-width: 700px) calc(100vw - 44px), 640px"
            alt=""
            width={props.item.media_w}
            height={props.item.media_h}
          />
        </span>
      </Show>
      <span class="reddit-provider-strip">
        <b>REDDIT</b>
        <i />
        <span>{externalHost(props.item.external_url)}</span>
        <strong>
          Open
          <Icon name="open-original" />
        </strong>
      </span>
    </a>
  );
}

function RedditImageCard(props: {
  item: Item;
  onClickThrough(): void;
  onImageChange(attempt: RedditResolvedImageAttempt | undefined): void;
}) {
  const [attempt, setAttempt] = createSignal<RedditImageAttempt>({
    sourceIndex: 0,
    decoding: "async",
  });
  const sources = createMemo(
    () => redditReaderImageSources(props.item),
    undefined,
    { equals: sameRedditImageSources },
  );
  let trackedSources = sources();
  createEffect(() => {
    const next = sources();
    if (next === trackedSources) return;
    trackedSources = next;
    setAttempt({ sourceIndex: 0, decoding: "async" });
  });
  const currentAttempt = createMemo(() => {
    const state = attempt();
    const source = sources()[state.sourceIndex];
    return source ? { ...state, source } : undefined;
  });
  createEffect(() => props.onImageChange(currentAttempt()));
  const target = () => props.item.external_url || props.item.url;

  const advanceSource = (current: RedditResolvedImageAttempt) => {
    if (currentAttempt() !== current) return;
    setAttempt({ sourceIndex: current.sourceIndex + 1, decoding: "async" });
  };

  const watchDecode = (
    current: RedditResolvedImageAttempt,
    image: HTMLImageElement,
  ) => {
    void decodeImageWithin(image, READER_IMAGE_DECODE_TIMEOUT_MS).then(
      (result) => {
        if (currentAttempt() !== current || result === "decoded") return;
        if (result === "timeout" && current.decoding === "async") {
          setAttempt({ sourceIndex: current.sourceIndex, decoding: "sync" });
          return;
        }
        advanceSource(current);
      },
    );
  };

  return (
    <div class="reddit-media-card reddit-image-card">
      <Show
        when={currentAttempt()}
        keyed
        fallback={
          <a
            href={target()}
            target="_blank"
            rel="noopener noreferrer"
            onClick={props.onClickThrough}
          >
            <span class="reddit-media-band reddit-image-band">
              <span class="reddit-image-unavailable">
                Image unavailable · open on Reddit
              </span>
            </span>
          </a>
        }
      >
        {(current) => (
          <Show
            when={current.source.kind === "stored"}
            fallback={
              <a
                href={target()}
                target="_blank"
                rel="noopener noreferrer"
                onClick={props.onClickThrough}
              >
                <span class="reddit-media-band reddit-image-band">
                  <RedditExternalImage
                    source={
                      (current.source as { kind: "external"; url: string }).url
                    }
                    alt={props.item.title}
                    decoding={current.decoding}
                    onLoad={(image) => watchDecode(current, image)}
                    onError={() => advanceSource(current)}
                  />
                </span>
              </a>
            }
          >
            <span class="reddit-media-band reddit-image-band lb-lead-host">
              <ResponsiveImage
                class="reddit-full-image reddit-lead-image"
                item={props.item}
                sizes="(max-width: 700px) calc(100vw - 44px), 640px"
                alt={props.item.title}
                width={props.item.media_w}
                height={props.item.media_h}
                loading="eager"
                decoding={current.decoding}
                onLoad={(event) => watchDecode(current, event.currentTarget)}
                onError={() => advanceSource(current)}
              />
            </span>
          </Show>
        )}
      </Show>
      <a
        class="reddit-provider-strip"
        href={target()}
        target="_blank"
        rel="noopener noreferrer"
        onClick={props.onClickThrough}
      >
        <b>REDDIT</b>
        <i />
        <span>{externalHost(target()) || "reddit.com"}</span>
        <strong>
          Open
          <Icon name="open-original" />
        </strong>
      </a>
    </div>
  );
}

type RedditImageDecoding = "async" | "sync";

const READER_IMAGE_DECODE_TIMEOUT_MS = 4_000;

interface RedditImageAttempt {
  sourceIndex: number;
  decoding: RedditImageDecoding;
}

interface RedditResolvedImageAttempt extends RedditImageAttempt {
  source: RedditReaderImageSource;
}

function sameRedditImageSources(
  previous: RedditReaderImageSource[] | undefined,
  next: RedditReaderImageSource[],
): boolean {
  return (
    previous?.length === next.length &&
    previous.every((source, index) => {
      const candidate = next[index];
      return (
        source.kind === candidate.kind &&
        (source.kind !== "external" ||
          (candidate.kind === "external" && source.url === candidate.url))
      );
    })
  );
}

function RedditExternalImage(props: {
  source: string;
  alt: string;
  decoding: RedditImageDecoding;
  onLoad(image: HTMLImageElement): void;
  onError(): void;
}) {
  let image: HTMLImageElement | undefined;
  onCleanup(() => image?.removeAttribute("src"));
  return (
    <img
      ref={image}
      class="reddit-full-image"
      src={props.source}
      alt={props.alt}
      loading="eager"
      decoding={props.decoding}
      referrerpolicy="no-referrer"
      onLoad={(event) => props.onLoad(event.currentTarget)}
      onError={props.onError}
    />
  );
}

function RedditSourceLine(props: { item: Item }) {
  return (
    <div class="reddit-source-line">
      <SourceBadge
        connector={props.item.connector}
        imageURL={props.item.favicon_url}
        title={props.item.feed_title}
        size={28}
      />
      <strong>{props.item.feed_title || "Reddit"}</strong>
      <span>
        · posted by {props.item.author || "unknown"} ·{" "}
        {relativeTime(props.item.display_date || props.item.published_ts)}
      </span>
    </div>
  );
}

function VideoMediaCard(props: { item: Item; onOriginal(): void }) {
  const displayURL = () =>
    props.item.url.replace(/^https?:\/\//, "").replace(/\/$/, "");
  return (
    <a
      class="video-media-card"
      href={props.item.url}
      target="_blank"
      rel="noopener noreferrer"
      onClick={props.onOriginal}
    >
      <span class="video-media-band">
        <Show when={props.item.media_url && props.item.item_id} keyed>
          {(_itemID) => (
            <ResponsiveImage
              item={props.item}
              sizes="(max-width: 700px) calc(100vw - 44px), 640px"
              alt=""
              width={props.item.media_w}
              height={props.item.media_h}
            />
          )}
        </Show>
        <span class="video-card-play" aria-hidden="true">
          <Icon name="play" size={20} filled />
        </span>
      </span>
      <span class="video-provider-strip">
        <b>YOUTUBE</b>
        <i />
        <span>{displayURL()}</span>
        <strong>
          Watch
          <Icon name="open-original" />
        </strong>
      </span>
    </a>
  );
}

function VideoDescription(props: {
  description: string;
  videoURL: string;
  onOriginal(): void;
}) {
  const blocks = () => parseVideoDescription(props.description, props.videoURL);
  return (
    <div class="video-description">
      <For each={blocks()}>
        {(block) => (
          <Show
            when={block.kind === "paragraph" ? block : undefined}
            fallback={
              <Show when={block.kind === "chapters" ? block : undefined}>
                {(chapters) => (
                  <div class="video-chapters">
                    <For each={chapters().rows}>
                      {(row) => (
                        <div>
                          <DescriptionTokenView
                            token={row.timestamp}
                            onOriginal={props.onOriginal}
                          />
                          <span>
                            <For each={row.tokens}>
                              {(token) => (
                                <DescriptionTokenView
                                  token={token}
                                  onOriginal={props.onOriginal}
                                />
                              )}
                            </For>
                          </span>
                        </div>
                      )}
                    </For>
                  </div>
                )}
              </Show>
            }
          >
            {(paragraph) => (
              <p>
                <For each={paragraph().tokens}>
                  {(token) => (
                    <DescriptionTokenView
                      token={token}
                      onOriginal={props.onOriginal}
                    />
                  )}
                </For>
              </p>
            )}
          </Show>
        )}
      </For>
    </div>
  );
}

function DescriptionTokenView(props: {
  token: DescriptionToken;
  onOriginal(): void;
}) {
  return (
    <Show when={props.token.kind !== "text"} fallback={props.token.text}>
      <a
        classList={{ timestamp: props.token.kind === "timestamp" }}
        href={props.token.kind === "text" ? undefined : props.token.href}
        target="_blank"
        rel="noopener noreferrer"
        onClick={props.onOriginal}
      >
        {props.token.text}
      </a>
    </Show>
  );
}

function OriginalRequired(props: {
  reason: "extraction" | "titles-only" | "paywall";
  url: string;
  retry?: () => void;
  onOriginal(): void;
}) {
  const sentence = () => {
    if (props.reason === "titles-only")
      return "This feed publishes titles only — there was never a body to fetch.";
    if (props.reason === "paywall")
      return "The publisher requires a subscription to read past the first paragraph.";
    return "Sema couldn't extract a clean body from this page — the text is split across script-rendered sections.";
  };
  return (
    <div class="original-required">
      <p>{sentence()}</p>
      <div>
        <a
          class="original-cta"
          href={props.url}
          target="_blank"
          rel="noopener noreferrer"
          onClick={props.onOriginal}
        >
          Read the original
          <Icon name="open-original" />
        </a>
        <Show when={props.retry}>
          {(retry) => (
            <button type="button" onClick={retry()}>
              <Icon name="retry" />
              Try again
            </button>
          )}
        </Show>
      </div>
    </div>
  );
}
