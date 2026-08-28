import {
  createEffect,
  createSignal,
  For,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import { Icon } from "../components/Icon";
import { isRedditItem, redditPrimaryRoute } from "../reddit-item";
import type { Item } from "../types";
import { relativeTime } from "./Grid";
import { ResponsiveImage } from "./ResponsiveImage";

export function RelatedPanel(props: {
  source: Item;
  items: Item[];
  loading: boolean;
  active: boolean;
  linkActionID: string;
  onClose(): void;
  onWalk(item: Item): void;
  onOpen(item: Item): void;
  onExternalOpen(item: Item): void;
  onDiscussion(item: Item): void;
  onSignal(item: Item, value: -1 | 0 | 1): void;
  onHeart(item: Item): void;
  onCopy(item: Item): void;
}) {
  let body!: HTMLDivElement;
  const [focusedID, setFocusedID] = createSignal("");

  createEffect(() => {
    props.source.item_id;
    setFocusedID(props.items[0]?.item_id ?? "");
    if (body) body.scrollTop = 0;
  });

  const move = (delta: number) => {
    if (props.items.length === 0) return;
    const current = Math.max(
      0,
      props.items.findIndex((item) => item.item_id === focusedID()),
    );
    const next =
      props.items[
        Math.max(0, Math.min(props.items.length - 1, current + delta))
      ];
    setFocusedID(next.item_id);
    requestAnimationFrame(() =>
      body
        .querySelector<HTMLElement>(
          `[data-related-id="${CSS.escape(next.item_id)}"]`,
        )
        ?.focus({ preventScroll: true }),
    );
  };

  const onKey = (event: KeyboardEvent) => {
    if (!props.active || event.metaKey || event.ctrlKey || event.altKey) return;
    const item = props.items.find(
      (candidate) => candidate.item_id === focusedID(),
    );
    if (event.key === "Escape") props.onClose();
    else if (event.key === "ArrowRight" || event.key === "j") move(1);
    else if (event.key === "ArrowLeft" || event.key === "k") move(-1);
    else if (event.key === "ArrowDown") move(2);
    else if (event.key === "ArrowUp") move(-2);
    else if (event.key === "r" && item) props.onWalk(item);
    else if (event.key === "Enter" && item) {
      const route = redditPrimaryRoute(item);
      if (route.kind === "external") {
        props.onExternalOpen(item);
        window.open(route.url, "_blank", "noopener,noreferrer");
      } else props.onOpen(item);
    } else return;
    event.preventDefault();
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

  onMount(() => window.addEventListener("keydown", onKey));
  onCleanup(() => window.removeEventListener("keydown", onKey));

  return (
    <div
      class="related-layer"
      role="presentation"
      onPointerDown={(event) =>
        event.target === event.currentTarget && props.onClose()
      }
    >
      <aside
        class="related-panel"
        role="dialog"
        aria-modal="true"
        aria-label={`Similar to ${props.source.title}`}
      >
        <i class="related-grabber" />
        <header>
          <div>
            <small>SIMILAR</small>
            <h2>{props.source.title}</h2>
          </div>
          <button
            type="button"
            aria-label="Close related items"
            onClick={props.onClose}
          >
            <Icon name="close" />
          </button>
        </header>
        <div class="related-body" ref={body}>
          <Show
            when={!props.loading}
            fallback={
              <div class="related-placeholders">
                <i />
                <i />
                <i />
                <i />
              </div>
            }
          >
            <Show
              when={props.items.length > 0}
              fallback={
                <div class="related-empty">
                  <h3>Nothing close yet.</h3>
                  <p>
                    Sema needs a few more items in this topic before it can find
                    neighbours.
                  </p>
                </div>
              }
            >
              <div class="related-grid">
                <For each={props.items}>
                  {(item) => (
                    <article
                      class="related-cell"
                      classList={{
                        focused: item.item_id === focusedID(),
                        kept: item.hearted,
                        "reddit-cell": isRedditItem(item),
                      }}
                      tabindex="-1"
                      data-related-id={item.item_id}
                      onFocus={() => setFocusedID(item.item_id)}
                      onMouseEnter={() => setFocusedID(item.item_id)}
                      onClick={() => openPrimary(item)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          openPrimary(item);
                        }
                      }}
                    >
                      <Show when={item.media_url}>
                        <ResponsiveImage
                          item={item}
                          sizes="(max-width: 700px) calc((100vw - 36px) / 2), 192px"
                          alt=""
                        />
                      </Show>
                      <Show
                        when={
                          item.media_type === "video" ||
                          (isRedditItem(item) &&
                            !!item.media_url &&
                            !!item.external_url)
                        }
                      >
                        <span class="related-video-play" aria-hidden="true">
                          <Icon
                            name={
                              item.media_type === "video" ||
                              item.post_type === "video"
                                ? "play"
                                : "open-original"
                            }
                            size={12}
                            filled={item.post_type === "video"}
                          />
                        </span>
                      </Show>
                      <div class="related-cell-scrim" />
                      <Show
                        when={item.hearted}
                        fallback={
                          <span class="similarity">≈{item.similarity}</span>
                        }
                      >
                        <span class="related-kept">
                          <Icon name="keep" size={14} filled={true} />
                        </span>
                      </Show>
                      <div class="related-actions">
                        <button
                          type="button"
                          aria-label="Boost"
                          onClick={(event) => {
                            event.stopPropagation();
                            props.onSignal(item, item.signal === 1 ? 0 : 1);
                          }}
                        >
                          <Icon name="boost" size={14} />
                        </button>
                        <button
                          type="button"
                          aria-label="Bury"
                          onClick={(event) => {
                            event.stopPropagation();
                            props.onSignal(item, item.signal === -1 ? 0 : -1);
                          }}
                        >
                          <Icon name="bury" size={14} />
                        </button>
                        <button
                          type="button"
                          aria-label="Keep"
                          onClick={(event) => {
                            event.stopPropagation();
                            props.onHeart(item);
                          }}
                        >
                          <Icon name="keep" size={14} filled={item.hearted} />
                        </button>
                        <button
                          type="button"
                          aria-label="Copy link"
                          onClick={(event) => {
                            event.stopPropagation();
                            props.onCopy(item);
                          }}
                        >
                          <Icon
                            name={
                              props.linkActionID === item.item_id
                                ? "check"
                                : "copy-link"
                            }
                            size={14}
                          />
                        </button>
                      </div>
                      <div class="related-copy">
                        <Show
                          when={
                            redditPrimaryRoute(item).kind === "external" &&
                            item.external_url
                          }
                          fallback={
                            <button
                              type="button"
                              onClick={(event) => {
                                event.stopPropagation();
                                props.onOpen(item);
                              }}
                            >
                              {item.title}
                            </button>
                          }
                        >
                          <a
                            href={item.external_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            onClick={(event) => {
                              event.stopPropagation();
                              props.onExternalOpen(item);
                            }}
                          >
                            {item.title}
                          </a>
                        </Show>
                        <span>
                          {item.feed_title || "Feed"} ·{" "}
                          {item.hearted
                            ? `kept ${relativeTime(item.hearted_ts || item.published_ts)} · ≈${item.similarity}`
                            : relativeTime(item.published_ts)}
                        </span>
                      </div>
                      <Show when={isRedditItem(item)}>
                        <a
                          class="reddit-discussion"
                          href={item.url}
                          target="_blank"
                          rel="noopener noreferrer"
                          aria-label="Discussion on Reddit"
                          data-tooltip="Discussion on Reddit"
                          onClick={(event) => {
                            event.stopPropagation();
                            props.onDiscussion(item);
                          }}
                        >
                          <Icon name="discussion" size={13} />
                        </a>
                      </Show>
                    </article>
                  )}
                </For>
              </div>
            </Show>
          </Show>
        </div>
      </aside>
    </div>
  );
}
