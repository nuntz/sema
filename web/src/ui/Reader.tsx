import { createEffect, createSignal, onCleanup, onMount, Show } from "solid-js";
import type { Item } from "../types";
import { relativeTime } from "./Grid";
import { readerCommand } from "./keyboard";
import { hasLeadingImage } from "./reader-content";

interface ReaderProps {
  item: Item;
  hearted: boolean;
  canPrevious: boolean;
  canNext: boolean;
  onClose(): void;
  onPrevious(): void;
  onNext(): void;
  onSignal(value: -1 | 0 | 1): void;
  onHeart(): void;
}

export function Reader(props: ReaderProps) {
  let article!: HTMLDivElement;
  const [body, setBody] = createSignal("");
  const [loading, setLoading] = createSignal(false);
  const [progress, setProgress] = createSignal(0);

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
    if (event.metaKey || event.ctrlKey || event.altKey) return;
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
      case "original":
        window.open(props.item.url, "_blank", "noopener,noreferrer");
        break;
      default:
        return;
    }
    event.preventDefault();
  };

  onMount(() => {
    window.addEventListener("keydown", onKey);
    article.addEventListener("scroll", updateProgress, { passive: true });
    onCleanup(() => {
      window.removeEventListener("keydown", onKey);
      article.removeEventListener("scroll", updateProgress);
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
          aria-label="Close reader"
        >
          ←
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
            classList={{ selected: props.item.signal === 1 }}
            onClick={() => props.onSignal(props.item.signal === 1 ? 0 : 1)}
          >
            ↑ up
          </button>
          <button
            type="button"
            classList={{ selected: props.item.signal === -1 }}
            onClick={() => props.onSignal(props.item.signal === -1 ? 0 : -1)}
          >
            ↓ down
          </button>
          <button
            type="button"
            class="keep"
            classList={{ selected: props.hearted }}
            onClick={props.onHeart}
          >
            ♥ keep
          </button>
          <i />
          <a href={props.item.url} target="_blank" rel="noopener noreferrer">
            original ↗
          </a>
          <button
            type="button"
            onClick={props.onPrevious}
            disabled={!props.canPrevious}
          >
            P
          </button>
          <button
            type="button"
            onClick={props.onNext}
            disabled={!props.canNext}
          >
            N
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
                  <a
                    class="original-cta"
                    href={props.item.url}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    Read the original ↗
                  </a>
                </Show>
              </div>
            }
          >
            <div class="article-body" innerHTML={body()} />
          </Show>
          <footer class="article-next">
            <span>NEXT UNREAD</span>
            <button
              type="button"
              onClick={props.onNext}
              disabled={!props.canNext}
            >
              {props.canNext
                ? "Continue to the next item →"
                : "You’re at the end"}
            </button>
          </footer>
        </article>
      </div>
      <nav class="reader-mobile-actions">
        <button
          type="button"
          classList={{ selected: props.item.signal === 1 }}
          onClick={() => props.onSignal(props.item.signal === 1 ? 0 : 1)}
        >
          ↑
        </button>
        <button
          type="button"
          classList={{ selected: props.item.signal === -1 }}
          onClick={() => props.onSignal(props.item.signal === -1 ? 0 : -1)}
        >
          ↓
        </button>
        <button
          type="button"
          class="keep"
          classList={{ selected: props.hearted }}
          onClick={props.onHeart}
        >
          ♥
        </button>
        <button
          type="button"
          class="next"
          onClick={props.onNext}
          disabled={!props.canNext}
        >
          NEXT →
        </button>
      </nav>
    </section>
  );
}
