import {
  createMemo,
  createSignal,
  For,
  type JSX,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import { Icon } from "../components/Icon";
import { whyText } from "../ranking-display";
import { externalHost, redditPrimaryRoute } from "../reddit-item";
import type { Item, Story } from "../types";
import { relativeTime, UnreadDot } from "./Grid";
import { ResponsiveImage } from "./ResponsiveImage";
import { SourceBadge } from "./SourceBadge";
import { blockRows, headlineSlice } from "./story-layout";

interface StoryBlocksProps {
  stories: Story[];
  focusedID: string;
  expandedStoryIDs: ReadonlySet<string>;
  onExpand(storyID: string): void;
  onFocus(id: string): void;
  onOpenLead(story: Story): void;
  onOpen(item: Item): void;
  onExternalOpen(item: Item): void;
  onHeart(item: Item): void;
}

export function StoryBlocks(props: StoryBlocksProps) {
  let region!: HTMLElement;
  const [width, setWidth] = createSignal(0);
  const rows = createMemo(() => blockRows(props.stories, width()));
  onMount(() => {
    const observer = new ResizeObserver(() => setWidth(region.clientWidth));
    observer.observe(region);
    setWidth(region.clientWidth);
    onCleanup(() => observer.disconnect());
  });
  return (
    <section ref={region} class="story-blocks" aria-label="Stories">
      <For each={rows()}>
        {(row, rowIndex) => (
          <div
            class="story-block-row"
            style={{ "grid-template-columns": row.template }}
          >
            <For each={row.stories}>
              {(story) => (
                <StoryCard
                  story={story}
                  tall={rowIndex() === 0}
                  focusedID={props.focusedID}
                  expanded={props.expandedStoryIDs.has(story.story_id)}
                  onExpand={() => props.onExpand(story.story_id)}
                  onFocus={props.onFocus}
                  onOpenLead={() => props.onOpenLead(story)}
                  onOpen={props.onOpen}
                  onExternalOpen={props.onExternalOpen}
                  onHeart={props.onHeart}
                />
              )}
            </For>
          </div>
        )}
      </For>
      <div class="single-source-rule" aria-hidden="true">
        <span>SINGLE-SOURCE</span>
        <i />
      </div>
    </section>
  );
}

function StoryCard(props: {
  story: Story;
  tall: boolean;
  focusedID: string;
  expanded: boolean;
  onExpand(): void;
  onFocus(id: string): void;
  onOpenLead(): void;
  onOpen(item: Item): void;
  onExternalOpen(item: Item): void;
  onHeart(item: Item): void;
}) {
  const lead = () => props.story.items[0];
  const focusID = () => `story:${props.story.story_id}`;
  const collapsed = createMemo(() => headlineSlice(props.story));
  const headlines = createMemo(() =>
    props.expanded ? props.story.items.slice(1) : collapsed().items,
  );
  const sourceLabel = () =>
    `${Math.min(props.story.source_count, 9)}${props.story.source_count > 9 ? "+" : ""} SOURCES`;
  return (
    <article
      class="story-card"
      classList={{
        focused: props.focusedID === focusID(),
        tall: props.tall,
        "no-media": !lead()?.media_url,
      }}
      data-focus-id={focusID()}
      data-story-id={props.story.story_id}
      onMouseEnter={() => props.onFocus(focusID())}
    >
      <Show when={lead()} keyed>
        {(item) => (
          <>
            <Show when={item.media_url}>
              <PrimaryAction
                item={item}
                class="story-media-action"
                tabIndex={-1}
                onOpen={props.onOpenLead}
                onExternalOpen={props.onExternalOpen}
              >
                <div class="story-media">
                  <ResponsiveImage
                    item={item}
                    sizes="(min-width: 1000px) 55vw, 100vw"
                    alt=""
                  />
                </div>
              </PrimaryAction>
            </Show>
            <div class="story-badges">
              <span>{sourceLabel()}</span>
              <Show when={item.size === "L"}>
                <em>top 10%</em>
              </Show>
            </div>
            <div class="story-corner">
              <UnreadDot visible={!item.read} />
              <span>{relativeTime(item.published_ts)}</span>
              <button
                type="button"
                class="story-heart"
                classList={{ selected: item.hearted }}
                aria-label={
                  item.hearted ? "Remove from archive" : "Keep in archive"
                }
                aria-pressed={item.hearted}
                onClick={(event) => {
                  event.stopPropagation();
                  props.onHeart(item);
                }}
              >
                <Icon name="keep" size={14} filled={item.hearted} />
              </button>
            </div>
            <PrimaryAction
              item={item}
              class="story-lead"
              onFocus={() => props.onFocus(focusID())}
              onOpen={props.onOpenLead}
              onExternalOpen={props.onExternalOpen}
            >
              <h2 classList={{ read: item.read }}>{item.title}</h2>
              <Show when={item.summary}>
                <p>{item.summary}</p>
              </Show>
              <div class="story-meta">
                <SourceBadge
                  connector={item.connector}
                  imageURL={item.favicon_url}
                  title={item.feed_title}
                  size={16}
                />
                <span>{item.feed_title || "Feed"}</span>
                <small>· {relativeTime(item.published_ts)}</small>
                <Show when={whyText(item)}>
                  <em title={whyText(item)}>{whyText(item)}</em>
                </Show>
              </div>
            </PrimaryAction>
          </>
        )}
      </Show>
      <div class="story-headlines">
        <For each={headlines()}>
          {(item) => (
            <PrimaryAction
              item={item}
              class="story-headline"
              classList={{
                focused: props.focusedID === item.item_id,
                read: item.read,
              }}
              data-focus-id={item.item_id}
              onFocus={() => props.onFocus(item.item_id)}
              onMouseEnter={() => props.onFocus(item.item_id)}
              onOpen={() => props.onOpen(item)}
              onExternalOpen={props.onExternalOpen}
            >
              <span class="story-headline-dot">
                <UnreadDot visible={!item.read} />
              </span>
              <SourceBadge
                connector={item.connector}
                imageURL={item.favicon_url}
                title={item.feed_title}
                size={12}
              />
              <span class="story-headline-feed">
                {item.feed_title || "Feed"}
              </span>
              <span class="story-headline-title">{item.title}</span>
              <time>{relativeTime(item.published_ts)}</time>
            </PrimaryAction>
          )}
        </For>
        <Show when={!props.expanded && collapsed().remaining > 0}>
          <button type="button" class="story-more" onClick={props.onExpand}>
            +{collapsed().remaining} more
          </button>
        </Show>
      </div>
    </article>
  );
}

function PrimaryAction(props: {
  item: Item;
  class: string;
  classList?: Record<string, boolean>;
  children: JSX.Element;
  onOpen(): void;
  onExternalOpen(item: Item): void;
  onFocus?(): void;
  onMouseEnter?(): void;
  tabIndex?: number;
  "data-focus-id"?: string;
}) {
  const route = createMemo(() => redditPrimaryRoute(props.item));
  return (
    <Show
      when={route().kind === "external" && props.item.external_url}
      fallback={
        <button
          type="button"
          class={props.class}
          classList={props.classList}
          data-focus-id={props["data-focus-id"]}
          tabIndex={props.tabIndex}
          onFocus={props.onFocus}
          onMouseEnter={props.onMouseEnter}
          onClick={props.onOpen}
        >
          {props.children}
        </button>
      }
    >
      <a
        class={props.class}
        classList={props.classList}
        data-focus-id={props["data-focus-id"]}
        tabIndex={props.tabIndex}
        href={props.item.external_url}
        target="_blank"
        rel="noopener noreferrer"
        aria-label={`Open ${props.item.title} on ${externalHost(props.item.external_url)}`}
        onFocus={props.onFocus}
        onMouseEnter={props.onMouseEnter}
        onClick={() => props.onExternalOpen(props.item)}
      >
        {props.children}
      </a>
    </Show>
  );
}
