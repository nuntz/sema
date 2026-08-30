import {
  createEffect,
  createMemo,
  createSignal,
  For,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import { isOlderThanThirtyDays } from "../archive";
import { AppHeader } from "../components/AppHeader";
import { Icon } from "../components/Icon";
import { createMediaQuery } from "../media-query";
import {
  externalHost,
  isRedditGallery,
  isRedditItem,
  redditReaderOriginalURL,
  redditSummaryProvenance,
  showsReaderOriginalFallback,
} from "../reddit-item";
import type { Item } from "../types";
import { relativeTime } from "./Grid";
import { readerCommand } from "./keyboard";
import { ResponsiveImage } from "./ResponsiveImage";
import { hasLeadingImage } from "./reader-content";
import { SourceBadge } from "./SourceBadge";
import {
  beginSwipe,
  lockSwipeAxis,
  type SwipeGesture,
  swipeCommand,
  swipeOffset,
} from "./touch-gestures";
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
  onClose(): void;
  onHome(): void;
  onPrevious(): void;
  onNext(): void;
  onSignal(value: -1 | 0 | 1): void;
  onHeart(): void;
  onCopy(): void;
  onOriginal(): void;
  onRelated(): void;
  onRetry(): void;
  onDwell(itemID: string, dwellMS: number): void;
}

export function Reader(props: ReaderProps) {
  let article!: HTMLDivElement;
  const [body, setBody] = createSignal("");
  const [loading, setLoading] = createSignal(false);
  const [progress, setProgress] = createSignal(0);
  const [scrolled, setScrolled] = createSignal(false);
  const [overflowOpen, setOverflowOpen] = createSignal(false);
  const [dragOffset, setDragOffset] = createSignal(0);
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
  let trackedID = props.item.item_id;
  let dwellMS = 0;
  let activeSince = 0;
  let lastReported = 0;
  let thresholdReported = false;
  let dwellTimer: number | undefined;
  let swipe: SwipeGesture | undefined;
  let touchX = 0;

  const originalReason = (): "extraction" | "titles-only" =>
    !props.item.summary && props.item.extract_quality === 0
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
    setOverflowOpen(false);
    setDragOffset(0);
    if (article) article.scrollTop = 0;
    startDwell();
  });

  createEffect(() => {
    const source = bodySource();
    const url = source.url;
    setBody("");
    if (!url || !source.hasBody) return;
    const controller = new AbortController();
    setLoading(true);
    fetch(url, { credentials: "same-origin", signal: controller.signal })
      .then((response) => {
        if (!response.ok) throw new Error("body unavailable");
        return response.text();
      })
      .then(setBody)
      .catch((error) => {
        if (error.name !== "AbortError") setBody("");
      })
      .finally(() => setLoading(false));
    onCleanup(() => controller.abort());
  });

  const updateProgress = () => {
    const range = article.scrollHeight - article.clientHeight;
    setProgress(range <= 0 ? 1 : article.scrollTop / range);
    setScrolled((current) =>
      current ? article.scrollTop > 160 : article.scrollTop >= 180,
    );
  };

  const onTouchStart = (event: TouchEvent) => {
    if (event.touches.length !== 1) return;
    const touch = event.touches[0];
    touchX = touch.clientX;
    swipe = beginSwipe(
      touch.clientX,
      touch.clientY,
      performance.now(),
      window.innerWidth,
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
    setDragOffset(
      swipeOffset(swipe, touch.clientX, props.canPrevious, props.canNext),
    );
  };

  const finishSwipe = () => {
    if (!swipe) return;
    const command = swipeCommand(swipe, touchX, performance.now());
    swipe = undefined;
    setSwiping(false);
    setDragOffset(0);
    if (command === "next" && props.canNext) props.onNext();
    if (command === "previous" && props.canPrevious) props.onPrevious();
  };

  const cancelSwipe = () => {
    swipe = undefined;
    setSwiping(false);
    setDragOffset(0);
  };

  const onKey = (event: KeyboardEvent) => {
    if (!props.active || event.metaKey || event.ctrlKey || event.altKey) return;
    switch (readerCommand(event.key)) {
      case "close":
        props.onClose();
        break;
      case "next":
        if (props.canNext) props.onNext();
        break;
      case "previous":
        if (props.canPrevious) props.onPrevious();
        break;
      case "like":
        props.onSignal(props.item.signal === 1 ? 0 : 1);
        break;
      case "dislike":
        props.onSignal(props.item.signal === -1 ? 0 : -1);
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
      window.clearInterval(dwellTimer);
    });
  });

  return (
    <section
      class="reader"
      role="dialog"
      aria-modal="true"
      aria-label={props.item.title}
    >
      <AppHeader
        view="reader"
        onHome={props.onHome}
        scrolled={scrolled()}
        progress={progress()}
      >
        <button
          type="button"
          class="reader-back"
          onClick={props.onClose}
          aria-label="Back to grid"
        >
          <Icon name="back-to-grid" />
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
            <span class="reader-crumb" aria-hidden={scrolled()}>
              <span class="reader-crumb__source">
                {props.item.feed_title || "Feed"}
              </span>{" "}
              ·{" "}
              {relativeTime(props.item.display_date || props.item.published_ts)}{" "}
              ago
            </span>
            <span class="reader-title" aria-hidden={!scrolled()}>
              {props.item.title}
            </span>
          </span>
        </div>
        <div class="chrome-group chrome-group--judge">
          <button
            type="button"
            class="chrome-btn chrome-btn--collapse-2"
            classList={{ "chrome-btn--on": props.item.signal === 1 }}
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
            class="chrome-btn chrome-btn--collapse-2"
            classList={{ "chrome-btn--on": props.item.signal === -1 }}
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
            class="chrome-btn"
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
            class="chrome-btn chrome-btn--collapse-1"
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
            class="chrome-btn chrome-btn--collapse-1"
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
          class="chrome-btn chrome-btn--original chrome-btn--collapse-1"
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
          classList={{ "is-hidden": !narrowHeader() }}
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
            class="chrome-btn chrome-btn--icon"
            onClick={props.onPrevious}
            disabled={!props.canPrevious}
            aria-label="Previous item"
          >
            <Icon name="previous-item" />
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
        <Show when={overflowOpen() && narrowHeader()}>
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
                  (body() || props.item.summary || "").split(/\s+/).length /
                    220,
                ),
              )}{" "}
              MIN READ
            </div>
          </Show>
          <h1>{props.item.title}</h1>
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
          <Show when={isRedditItem(props.item)}>
            <RedditReaderIntro
              item={props.item}
              onClickThrough={props.onOriginal}
            />
          </Show>
          <Show when={props.item.summary}>
            <div class="article-summary">
              <Show when={props.item.summary_source === "generated"}>
                <div class="summary-provenance">
                  {isRedditItem(props.item)
                    ? redditSummaryProvenance(props.item)
                    : "summary · generated"}
                </div>
              </Show>
              <p>{props.item.summary}</p>
            </div>
          </Show>
          <Show
            when={
              props.item.media_type !== "video" &&
              !isRedditItem(props.item) &&
              props.item.media_url &&
              !hasLeadingImage(body())
            }
          >
            <ResponsiveImage
              class="article-lead"
              item={props.item}
              sizes="(max-width: 700px) calc(100vw - 44px), 640px"
              alt=""
              width={props.item.media_w}
              height={props.item.media_h}
            />
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
            <div class="article-body" innerHTML={body()} />
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
      <nav class="reader-bottom-actions" aria-label="Article actions">
        <button
          type="button"
          class="chrome-btn chrome-btn--icon"
          classList={{ "chrome-btn--on": props.item.signal === 1 }}
          aria-label={props.item.signal === 1 ? "Remove boost" : "Boost"}
          aria-pressed={props.item.signal === 1}
          onClick={() => props.onSignal(props.item.signal === 1 ? 0 : 1)}
        >
          <Icon name="boost" />
        </button>
        <button
          type="button"
          class="chrome-btn chrome-btn--icon"
          classList={{ "chrome-btn--on": props.item.signal === -1 }}
          aria-label={props.item.signal === -1 ? "Remove bury" : "Bury"}
          aria-pressed={props.item.signal === -1}
          onClick={() => props.onSignal(props.item.signal === -1 ? 0 : -1)}
        >
          <Icon name="bury" />
        </button>
        <button
          type="button"
          class="chrome-btn chrome-btn--icon"
          classList={{ "chrome-btn--on": props.hearted }}
          aria-label={props.hearted ? "Remove from archive" : "Keep in archive"}
          aria-pressed={props.hearted}
          onClick={props.onHeart}
        >
          <Icon name="keep" filled={props.hearted} />
        </button>
        <span class="reader-bottom-actions__spacer" />
        <button
          type="button"
          class="chrome-btn chrome-btn--icon"
          onClick={props.onPrevious}
          disabled={!props.canPrevious}
          aria-label="Previous item"
        >
          <Icon name="previous-item" />
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
      </nav>
    </section>
  );
}

function RedditReaderIntro(props: { item: Item; onClickThrough(): void }) {
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

function RedditImageCard(props: { item: Item; onClickThrough(): void }) {
  const [failedURL, setFailedURL] = createSignal("");
  const gallery = () => isRedditGallery(props.item);
  const destination = () => props.item.external_url || "";
  const useStoredImage = () =>
    gallery() || !destination() || failedURL() === destination();
  const target = () => (useStoredImage() ? props.item.url : destination());
  return (
    <a
      class="reddit-media-card reddit-image-card"
      href={target()}
      target="_blank"
      rel="noopener noreferrer"
      onClick={props.onClickThrough}
    >
      <span class="reddit-media-band reddit-image-band">
        <Show
          when={!useStoredImage()}
          fallback={
            <Show
              when={props.item.media_url}
              fallback={
                <span class="reddit-image-unavailable">
                  Image unavailable · open on Reddit
                </span>
              }
            >
              <ResponsiveImage
                item={props.item}
                sizes="(max-width: 700px) calc(100vw - 44px), 640px"
                alt={props.item.title}
                width={props.item.media_w}
                height={props.item.media_h}
              />
            </Show>
          }
        >
          <img
            class="reddit-full-image"
            src={destination()}
            alt={props.item.title}
            loading="eager"
            decoding="async"
            referrerpolicy="no-referrer"
            onError={() => setFailedURL(destination())}
          />
        </Show>
      </span>
      <span class="reddit-provider-strip">
        <b>REDDIT</b>
        <i />
        <span>
          {useStoredImage() ? "reddit.com" : externalHost(destination())}
        </span>
        <strong>
          {useStoredImage() ? "Open Reddit" : "Open"}
          <Icon name="open-original" />
        </strong>
      </span>
    </a>
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
        <Show when={props.item.media_url}>
          <ResponsiveImage
            item={props.item}
            sizes="(max-width: 700px) calc(100vw - 44px), 640px"
            alt=""
            width={props.item.media_w}
            height={props.item.media_h}
          />
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
