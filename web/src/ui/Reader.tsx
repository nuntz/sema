import { createEffect, createSignal, onCleanup, onMount, Show } from "solid-js";
import { isOlderThanThirtyDays } from "../archive";
import { Icon } from "../components/Icon";
import type { Item } from "../types";
import { relativeTime } from "./Grid";
import { readerCommand } from "./keyboard";
import { hasLeadingImage } from "./reader-content";

interface ReaderProps {
  item: Item;
  active: boolean;
  archive: boolean;
  hearted: boolean;
  linkActionActive: boolean;
  canPrevious: boolean;
  canNext: boolean;
  onClose(): void;
  onPrevious(): void;
  onNext(): void;
  onSignal(value: -1 | 0 | 1): void;
  onHeart(): void;
  onCopy(): void;
  onOriginal(): void;
  onDwell(itemID: string, dwellMS: number): void;
}

export function Reader(props: ReaderProps) {
  let article!: HTMLDivElement;
  const [body, setBody] = createSignal("");
  const [loading, setLoading] = createSignal(false);
  const [progress, setProgress] = createSignal(0);
  let trackedID = props.item.item_id;
  let dwellMS = 0;
  let activeSince = 0;
  let lastReported = 0;
  let thresholdReported = false;
  let dwellTimer: number | undefined;

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
    startDwell();
  });

  createEffect(() => {
    const url = props.item.body_url;
    setBody("");
    if (!url || !props.item.has_body) return;
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
    onCleanup(() => {
      pauseAndReport();
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("focus", onFocus);
      window.removeEventListener("blur", onBlur);
      document.removeEventListener("visibilitychange", onVisibility);
      article.removeEventListener("scroll", updateProgress);
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
      <header class="reader-bar">
        <button
          type="button"
          class="reader-back"
          onClick={props.onClose}
          aria-label="Back to grid"
        >
          <Icon name="back-to-grid" />
        </button>
        <Show when={props.item.favicon_url}>
          <img class="reader-favicon" src={props.item.favicon_url} alt="" />
        </Show>
        <span class="reader-source">
          {props.item.feed_title || "Feed"} ·{" "}
          {relativeTime(props.item.published_ts)} ago
        </span>
        <div class="reader-actions">
          <button
            type="button"
            class="signal"
            classList={{ selected: props.item.signal === 1 }}
            aria-pressed={props.item.signal === 1}
            onClick={() => props.onSignal(props.item.signal === 1 ? 0 : 1)}
          >
            <Icon name="thumbs-up" filled={props.item.signal === 1} />
            up
          </button>
          <button
            type="button"
            class="signal"
            classList={{ selected: props.item.signal === -1 }}
            aria-pressed={props.item.signal === -1}
            onClick={() => props.onSignal(props.item.signal === -1 ? 0 : -1)}
          >
            <Icon name="thumbs-down" filled={props.item.signal === -1} />
            down
          </button>
          <button
            type="button"
            class="keep"
            classList={{ selected: props.hearted }}
            aria-pressed={props.hearted}
            onClick={props.onHeart}
          >
            <Icon name="keep" filled={props.hearted} />
            {props.hearted ? "kept" : "keep"}
          </button>
          <button
            type="button"
            class="copy-link"
            classList={{ activated: props.linkActionActive }}
            onClick={props.onCopy}
          >
            <span>
              {props.linkActionActive ? (
                <Icon name="check" />
              ) : (
                <Icon name="copy-link" />
              )}
            </span>
            copy link
          </button>
          <i />
          <a
            href={props.item.url}
            target="_blank"
            rel="noopener noreferrer"
            class="original-link"
            onClick={props.onOriginal}
          >
            original
            <Icon name="open-original" />
          </a>
          <button
            type="button"
            onClick={props.onPrevious}
            disabled={!props.canPrevious}
            aria-label="Previous item"
            title="Previous item"
          >
            <Icon name="previous-item" />
          </button>
          <button
            type="button"
            onClick={props.onNext}
            disabled={!props.canNext}
            aria-label="Next item"
            title="Next item"
          >
            <Icon name="next-item" />
          </button>
        </div>
      </header>
      <div class="reader-progress">
        <i style={{ width: `${progress() * 100}%` }} />
      </div>
      <div class="reader-scroll" ref={article}>
        <article class="article">
          <div class="article-kicker">
            ARTICLE ·{" "}
            {Math.max(
              1,
              Math.round(
                (body() || props.item.summary || "").split(/\s+/).length / 220,
              ),
            )}{" "}
            MIN READ
          </div>
          <h1>{props.item.title}</h1>
          <p class="byline">
            {props.item.author
              ? `By ${props.item.author}`
              : props.item.feed_title}
          </p>
          <Show when={props.item.media_url && !hasLeadingImage(body())}>
            <img
              class="article-lead"
              src={props.item.media_url}
              alt=""
              width={props.item.media_w}
              height={props.item.media_h}
            />
          </Show>
          <Show
            when={body()}
            fallback={
              <div class="article-fallback">
                <Show when={loading()}>
                  <p>Loading the extracted article…</p>
                </Show>
                <Show when={!loading()}>
                  <p>
                    {props.item.summary ||
                      "Sema could not extract this article."}
                  </p>
                  <Show when={!props.archive}>
                    <a
                      class="original-cta"
                      href={props.item.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      onClick={props.onOriginal}
                    >
                      Read the original
                      <Icon name="open-original" />
                    </a>
                  </Show>
                </Show>
              </div>
            }
          >
            <div class="article-body" innerHTML={body()} />
          </Show>
          <Show when={props.archive}>
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
      <nav class="reader-mobile-actions">
        <button
          type="button"
          classList={{ selected: props.item.signal === 1 }}
          aria-label="Thumbs up"
          aria-pressed={props.item.signal === 1}
          onClick={() => props.onSignal(props.item.signal === 1 ? 0 : 1)}
        >
          <Icon name="thumbs-up" size={18} filled={props.item.signal === 1} />
        </button>
        <button
          type="button"
          classList={{ selected: props.item.signal === -1 }}
          aria-label="Thumbs down"
          aria-pressed={props.item.signal === -1}
          onClick={() => props.onSignal(props.item.signal === -1 ? 0 : -1)}
        >
          <Icon
            name="thumbs-down"
            size={18}
            filled={props.item.signal === -1}
          />
        </button>
        <button
          type="button"
          class="keep"
          classList={{ selected: props.hearted }}
          aria-label={props.hearted ? "Remove from archive" : "Keep in archive"}
          aria-pressed={props.hearted}
          onClick={props.onHeart}
        >
          <Icon name="keep" size={18} filled={props.hearted} />
        </button>
        <button
          type="button"
          class="next"
          onClick={props.onNext}
          disabled={!props.canNext}
        >
          NEXT
          <Icon name="next-item" size={18} />
        </button>
        <button
          type="button"
          class="copy-link"
          classList={{ activated: props.linkActionActive }}
          aria-label="Copy or share original link"
          onClick={props.onCopy}
        >
          {props.linkActionActive ? (
            <Icon name="check" size={18} />
          ) : (
            <Icon name="copy-link" size={18} />
          )}
        </button>
      </nav>
    </section>
  );
}
