import { createMemo, For, type JSX, Show } from "solid-js";
import { Icon } from "../components/Icon";
import type { LayoutCell, LayoutRow } from "../layout/justified";
import { type ReadStateContext, readVisualState } from "../layout/read-state";
import { whyText } from "../ranking-display";
import { externalHost, redditPrimaryRoute } from "../reddit-item";
import type { Item, Story } from "../types";
import { relativeTime, UnreadDot } from "./Grid";
import { ResponsiveImage } from "./ResponsiveImage";
import { SourceBadge } from "./SourceBadge";

interface StoryCellProps {
  story: Story;
  cell: LayoutCell;
  row: LayoutRow;
  focusedID: string;
  readContext: ReadStateContext;
  onExpand(storyID: string): void;
  onFocus(id: string): void;
  onOpenLead(story: Story): void;
  onOpen(item: Item): void;
  onExternalOpen(item: Item): void;
  onHeart(item: Item): void;
}

export function StoryCell(props: StoryCellProps) {
  const lead = () => props.story.items[0];
  const focusID = () => `story:${props.story.story_id}`;
  const headlines = createMemo(() =>
    props.story.items.slice(1, 1 + (props.cell.headlineItemCount ?? 0)),
  );
  const remaining = () => props.cell.headlineRemaining ?? 0;
  const showHeadlines = () => (props.cell.headlineHeight ?? 0) > 0;
  const cellHeight = () => props.cell.height ?? props.row.height;
  const leadHeight = () => cellHeight() - (props.cell.headlineHeight ?? 0);
  const compact = () => props.story.size !== "L" || leadHeight() < 230;
  const fullyRead = () => props.story.items.every((item) => item.read);
  const cellReadVisuals = createMemo(() =>
    readVisualState(props.readContext, fullyRead()),
  );
  const sourceLabel = () =>
    `${Math.min(props.story.source_count, 9)}${props.story.source_count > 9 ? "+" : ""} SOURCES`;
  const focusLeadUnlessHeadline = (target: EventTarget | null) => {
    if (
      !(target instanceof Element) ||
      (!target.closest(".story-headlines") &&
        !target.closest(".story-heart") &&
        !target.closest(".story-expand"))
    )
      props.onFocus(focusID());
  };

  return (
    <article
      class="grid-cell story-card story-cell"
      classList={{
        focused: props.focusedID === focusID(),
        read: cellReadVisuals().dimmed,
        compact: compact(),
        "no-media": !lead()?.media_url,
        [`size-${props.story.size.toLowerCase()}`]: true,
      }}
      style={{
        left: `${props.cell.left}px`,
        top: `${props.cell.offsetY ?? 0}px`,
        width: `${props.cell.width}px`,
        height: `${cellHeight()}px`,
      }}
      data-item-id={focusID()}
      data-focus-id={focusID()}
      data-story-id={props.story.story_id}
      onFocus={(event) => focusLeadUnlessHeadline(event.target)}
      onMouseOver={(event) => focusLeadUnlessHeadline(event.target)}
    >
      <Show when={lead()} keyed>
        {(item) => {
          const leadReadVisuals = createMemo(() =>
            readVisualState(props.readContext, item.read),
          );
          return (
            <div
              class="story-lead-shell"
              style={{ height: `${leadHeight()}px` }}
            >
              <Show when={item.media_url}>
                <PrimaryAction
                  item={item}
                  class="story-media-action"
                  tabIndex={-1}
                  onOpen={() => props.onOpenLead(props.story)}
                  onExternalOpen={props.onExternalOpen}
                >
                  <div class="story-media">
                    <ResponsiveImage
                      item={item}
                      sizes={props.cell.width}
                      alt=""
                      loading="lazy"
                    />
                  </div>
                </PrimaryAction>
              </Show>
              <div class="story-badges">
                <span>{sourceLabel()}</span>
                <Show when={props.story.size === "L"}>
                  <em>top 10%</em>
                </Show>
              </div>
              <div class="story-corner">
                <UnreadDot visible={leadReadVisuals().unreadDot} />
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
                onOpen={() => props.onOpenLead(props.story)}
                onExternalOpen={props.onExternalOpen}
              >
                <h2 classList={{ read: leadReadVisuals().dimmed }}>
                  {item.title}
                </h2>
                <Show when={!compact() && item.summary}>
                  <p>{item.summary}</p>
                </Show>
                <Show when={!compact()}>
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
                </Show>
              </PrimaryAction>
              <Show when={!showHeadlines() && props.story.items.length > 1}>
                <button
                  type="button"
                  class="story-expand"
                  aria-label={`Show ${props.story.items.length - 1} related headlines`}
                  onClick={(event) => {
                    event.stopPropagation();
                    props.onExpand(props.story.story_id);
                  }}
                >
                  +{props.story.items.length - 1}
                </button>
              </Show>
            </div>
          );
        }}
      </Show>
      <Show when={showHeadlines()}>
        <div class="story-headlines">
          <For each={headlines()}>
            {(item) => {
              const readVisuals = createMemo(() =>
                readVisualState(props.readContext, item.read),
              );
              return (
                <PrimaryAction
                  item={item}
                  class="story-headline"
                  classList={{
                    focused: props.focusedID === item.item_id,
                    read: readVisuals().dimmed,
                  }}
                  data-focus-id={item.item_id}
                  onFocus={() => props.onFocus(item.item_id)}
                  onMouseEnter={() => props.onFocus(item.item_id)}
                  onOpen={() => props.onOpen(item)}
                  onExternalOpen={props.onExternalOpen}
                >
                  <span class="story-headline-dot">
                    <UnreadDot visible={readVisuals().unreadDot} />
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
              );
            }}
          </For>
          <Show when={remaining() > 0}>
            <button
              type="button"
              class="story-more"
              onClick={() => props.onExpand(props.story.story_id)}
            >
              +{remaining()} more
            </button>
          </Show>
        </div>
      </Show>
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
