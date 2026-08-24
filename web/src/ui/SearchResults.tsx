import { createMemo, For, onCleanup, onMount, Show } from "solid-js";
import { Icon } from "../components/Icon";
import { type SearchSection, visibleSearchSections } from "../search";
import type { Item, SearchResponse } from "../types";
import { relativeTime } from "./Grid";
import { SourceBadge } from "./SourceBadge";

interface SearchResultsProps {
  query: string;
  response?: SearchResponse;
  loading: boolean;
  focusedID: string;
  active: boolean;
  linkActionID: string;
  onFocus(id: string): void;
  onOpen(item: Item, archive: boolean): void;
  onSignal(item: Item, value: -1 | 0 | 1): void;
  onHeart(item: Item): void;
  onCopy(item: Item): void;
  onRelated(item: Item): void;
  onEscape(): void;
}

export function SearchResults(props: SearchResultsProps) {
  let scroller!: HTMLDivElement;
  const sections = createMemo<SearchSection[]>(() =>
    visibleSearchSections(props.response),
  );
  const linear = createMemo(() =>
    sections().flatMap((section) =>
      section.items.map((item) => ({ item, archive: section.archive })),
    ),
  );

  const move = (delta: number) => {
    const items = linear();
    if (items.length === 0) return;
    const current = Math.max(
      0,
      items.findIndex(({ item }) => item.item_id === props.focusedID),
    );
    const next =
      items[Math.max(0, Math.min(items.length - 1, current + delta))];
    props.onFocus(next.item.item_id);
    requestAnimationFrame(() => {
      scroller
        .querySelector<HTMLElement>(
          `[data-search-id="${CSS.escape(next.item.item_id)}"]`,
        )
        ?.focus({ preventScroll: true });
      scroller
        .querySelector<HTMLElement>(
          `[data-search-id="${CSS.escape(next.item.item_id)}"]`,
        )
        ?.scrollIntoView({ block: "nearest", inline: "nearest" });
    });
  };

  const onKey = (event: KeyboardEvent) => {
    if (!props.active || event.metaKey || event.ctrlKey || event.altKey) return;
    const targetIsInput =
      event.target instanceof HTMLElement &&
      event.target.matches("input, textarea");
    if (event.key === "Escape") {
      props.onEscape();
    } else if (
      event.key === "ArrowDown" ||
      (!targetIsInput && event.key === "j")
    ) {
      move(1);
    } else if (
      event.key === "ArrowUp" ||
      (!targetIsInput && event.key === "k")
    ) {
      move(-1);
    } else if (
      !targetIsInput &&
      (event.key === "ArrowRight" || event.key === "ArrowLeft")
    ) {
      move(event.key === "ArrowRight" ? 1 : -1);
    } else if (!targetIsInput && event.key === "Enter") {
      const current = linear().find(
        ({ item }) => item.item_id === props.focusedID,
      );
      if (current) props.onOpen(current.item, current.archive);
    } else if (!targetIsInput && event.key === "r") {
      const current = linear().find(
        ({ item }) => item.item_id === props.focusedID,
      );
      if (current) props.onRelated(current.item);
    } else {
      return;
    }
    event.preventDefault();
  };

  onMount(() => window.addEventListener("keydown", onKey));
  onCleanup(() => window.removeEventListener("keydown", onKey));

  const empty = () => !props.loading && sections().length === 0;
  const quotedQuery = () => [...props.query].slice(0, 40).join("");

  return (
    <section class="search-results" ref={scroller} aria-label="Search results">
      <Show
        when={!empty()}
        fallback={
          <SearchEmpty
            query={quotedQuery()}
            semantic={props.response?.semantic_available !== false}
          />
        }
      >
        <For each={sections()}>
          {(section) => (
            <section class="search-section" data-section={section.key}>
              <header>
                <span>
                  {section.label} · {section.suffix}
                </span>
                <i />
                <small>{section.items.length}</small>
              </header>
              <div class="search-cell-grid">
                <For each={section.items}>
                  {(item) => (
                    <ResultCell
                      item={item}
                      archive={section.archive}
                      focused={item.item_id === props.focusedID}
                      linkActionActive={item.item_id === props.linkActionID}
                      onFocus={() => props.onFocus(item.item_id)}
                      onOpen={() => props.onOpen(item, section.archive)}
                      onSignal={(value) => props.onSignal(item, value)}
                      onHeart={() => props.onHeart(item)}
                      onCopy={() => props.onCopy(item)}
                      onRelated={() => props.onRelated(item)}
                    />
                  )}
                </For>
              </div>
            </section>
          )}
        </For>
        <Show
          when={
            props.loading &&
            sections().every((section) => !section.key.startsWith("related"))
          }
        >
          <section class="search-section related-placeholder">
            <header>
              <span>Related</span>
              <i />
              <small />
            </header>
            <div>
              <i />
              <i />
            </div>
          </section>
        </Show>
      </Show>
    </section>
  );
}

function ResultCell(props: {
  item: Item;
  archive: boolean;
  focused: boolean;
  linkActionActive: boolean;
  onFocus(): void;
  onOpen(): void;
  onSignal(value: -1 | 0 | 1): void;
  onHeart(): void;
  onCopy(): void;
  onRelated(): void;
}) {
  return (
    <article
      class="grid-cell result-cell size-m"
      classList={{
        focused: props.focused,
        "archive-cell": props.archive,
        "text-cell": !props.item.media_url,
        "video-cell": props.item.media_type === "video",
      }}
      tabindex="-1"
      data-search-id={props.item.item_id}
      onFocus={props.onFocus}
      onMouseEnter={props.onFocus}
    >
      <Show when={props.item.media_url}>
        <img src={props.item.media_url} alt="" />
      </Show>
      <Show when={props.item.media_type === "video"}>
        <span class="video-play" aria-hidden="true">
          <Icon name="play" size={14} filled />
        </span>
      </Show>
      <div class="cell-scrim" />
      <div class="cell-corner">
        <Show
          when={props.archive}
          fallback={
            props.item.similarity !== undefined ? (
              <span class="similarity">≈{props.item.similarity}</span>
            ) : (
              <span class="cell-age">
                {relativeTime(props.item.published_ts)}
              </span>
            )
          }
        >
          <span class="kept-marker">
            <Icon name="keep" size={14} filled={true} />
          </span>
        </Show>
      </div>
      <div class="cell-actions">
        <button
          type="button"
          classList={{ selected: props.item.signal === 1 }}
          aria-label="Thumb up"
          onClick={() => props.onSignal(props.item.signal === 1 ? 0 : 1)}
        >
          <Icon name="thumbs-up" size={14} filled={props.item.signal === 1} />
        </button>
        <button
          type="button"
          classList={{ selected: props.item.signal === -1 }}
          aria-label="Thumb down"
          onClick={() => props.onSignal(props.item.signal === -1 ? 0 : -1)}
        >
          <Icon
            name="thumbs-down"
            size={14}
            filled={props.item.signal === -1}
          />
        </button>
        <button
          type="button"
          classList={{ selected: props.item.hearted }}
          aria-label="Keep"
          onClick={props.onHeart}
        >
          <Icon name="keep" size={14} filled={props.item.hearted} />
        </button>
        <button
          type="button"
          aria-label="More like this"
          onClick={props.onRelated}
        >
          <Icon name="search" size={14} />
        </button>
        <button
          type="button"
          classList={{ activated: props.linkActionActive }}
          aria-label="Copy link"
          onClick={props.onCopy}
        >
          <Icon
            name={props.linkActionActive ? "check" : "copy-link"}
            size={14}
          />
        </button>
      </div>
      <button type="button" class="cell-main" onClick={props.onOpen}>
        <div class="cell-copy">
          <h2>{props.item.title}</h2>
          <Show when={props.item.summary}>
            <p>{props.item.summary}</p>
          </Show>
          <div class="cell-meta">
            <SourceBadge
              connector={props.item.connector}
              imageURL={props.item.favicon_url}
              title={props.item.feed_title}
              size={16}
            />
            <span>
              {props.item.feed_title || "Feed"} ·{" "}
              {props.archive
                ? `kept ${relativeTime(props.item.hearted_ts || props.item.published_ts)}`
                : relativeTime(props.item.published_ts)}
            </span>
            <Show when={props.archive && props.item.similarity !== undefined}>
              <em>≈{props.item.similarity}</em>
            </Show>
          </div>
        </div>
      </button>
    </article>
  );
}

function SearchEmpty(props: { query: string; semantic: boolean }) {
  return (
    <section class="search-empty">
      <h2>No matches for “{props.query}”.</h2>
      <p>
        {props.semantic
          ? "Related works from meaning rather than spelling — try a few words about the topic."
          : "Meaning search is unavailable right now — literal matching only."}
      </p>
    </section>
  );
}
