import {
  createMemo,
  createSignal,
  For,
  type JSX,
  onCleanup,
  Show,
} from "solid-js";
import { Icon } from "../components/Icon";
import {
  gridSourceName,
  headlineText,
  repeatsLeadHeadline,
} from "../grid-display";
import {
  type LayoutCell,
  type LayoutRow,
  storyCardBorderHeight,
} from "../layout/justified";
import { type ReadStateContext, readVisualState } from "../layout/read-state";
import { whyText } from "../ranking-display";
import { externalHost, redditPrimaryRoute } from "../reddit-item";
import type { Item, Story } from "../types";
import { CellCopy, relativeTime, UnreadDot } from "./Grid";
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

interface StoryCellProps {
  story: Story;
  cell: LayoutCell;
  row: LayoutRow;
  focusedID: string;
  readContext: ReadStateContext;
  refined?: boolean;
  pressed: boolean;
  onExpand(storyID: string): void;
  onLeadHeight(storyID: string, height: number): void;
  onFocus(id: string): void;
  onOpenLead(story: Story): void;
  onOpen(item: Item): void;
  onExternalOpen(item: Item): void;
  onHeart(item: Item): void;
  onSignal(item: Item, value: -1 | 0 | 1): void;
  onApplyFeed(item: Item): void;
  onMore(story: Story): void;
  onLongPressStart(event: PointerEvent, story: Story): void;
  onLongPressMove(event: PointerEvent): void;
  onLongPressEnd(): void;
}

export function StoryCell(props: StoryCellProps) {
  const lead = () => props.story.items[0];
  const signalFresh = createSignalFresh(() => lead()?.signal ?? 0);
  const focusID = () => `story:${props.story.story_id}`;
  const headlines = createMemo(() =>
    props.story.items.slice(1, 1 + (props.cell.headlineItemCount ?? 0)),
  );
  const remaining = () => props.cell.headlineRemaining ?? 0;
  const showHeadlines = () => (props.cell.headlineHeight ?? 0) > 0;
  const cellHeight = () => props.cell.height ?? props.row.height;
  const leadHeight = () =>
    cellHeight() -
    (props.cell.headlineHeight ?? 0) -
    (props.cell.mobileStoryCard && !props.refined ? 0 : storyCardBorderHeight);
  const editorial = () =>
    props.story.size === "L" && props.cell.mobileTile !== true;
  const [copyMetrics, setCopyMetrics] = createSignal({
    height: 0,
    mediaHeight: 0,
    summaryLineHeight: 0,
    summaryMargin: 0,
  });
  const summaryLines = () => {
    const metrics = copyMetrics();
    if (props.cell.mobileStoryCard || !metrics.summaryLineHeight) return 0;
    const available = leadHeight() - metrics.height - metrics.mediaHeight;
    return Math.max(
      0,
      Math.min(
        3,
        Math.floor(
          (available - metrics.summaryMargin) / metrics.summaryLineHeight,
        ),
      ),
    );
  };
  const measureTitle = (title: HTMLHeadingElement) => {
    // Keep optional summaries out of the minimum height to avoid a layout loop.
    const observer = new ResizeObserver(() => {
      if (props.cell.mobileStoryCard && !props.refined) return;
      const copy = title.parentElement;
      const meta = copy?.querySelector<HTMLElement>(".story-meta");
      if (!copy || !meta) return;
      observer.observe(meta);
      const style = getComputedStyle(copy);
      const height =
        title.getBoundingClientRect().height +
        Number.parseFloat(style.paddingTop) +
        Number.parseFloat(style.paddingBottom) +
        meta.getBoundingClientRect().height +
        Number.parseFloat(getComputedStyle(meta).marginTop);
      const summary = copy.querySelector("p");
      const summaryStyle = summary ? getComputedStyle(summary) : undefined;
      const media = copy.parentElement?.querySelector(".story-media-action");
      const mediaStyle = media ? getComputedStyle(media) : undefined;
      setCopyMetrics({
        height,
        mediaHeight: Number.parseFloat(mediaStyle?.maxHeight ?? "0"),
        summaryLineHeight: Number.parseFloat(summaryStyle?.lineHeight ?? "0"),
        summaryMargin: Number.parseFloat(summaryStyle?.marginTop ?? "0"),
      });
      props.onLeadHeight(
        props.story.story_id,
        Math.ceil(height + Number.parseFloat(mediaStyle?.minHeight ?? "0")),
      );
    });
    observer.observe(title);
    onCleanup(() => observer.disconnect());
  };
  const fullyRead = () => props.story.items.every((item) => item.read);
  const cellReadVisuals = createMemo(() =>
    readVisualState(props.readContext, fullyRead()),
  );
  const sourceLabel = () =>
    `${Math.min(props.story.source_count, 9)}${props.story.source_count > 9 ? "+" : ""} SOURCES`;
  const pointerDown = (event: PointerEvent) => {
    const target = event.target as HTMLElement;
    if (
      target.closest(
        ".story-headlines, .story-actions, .cell-actions, .story-more, .story-stack-badge",
      )
    )
      return;
    props.onLongPressStart(event, props.story);
  };
  const focusLeadUnlessHeadline = (target: EventTarget | null) => {
    if (
      !(target instanceof Element) ||
      (!target.closest(".story-headlines") &&
        !target.closest(".story-actions") &&
        !target.closest(".cell-actions") &&
        !target.closest(".story-expand"))
    )
      props.onFocus(focusID());
  };

  return (
    <article
      class="grid-cell story-cell"
      classList={{
        "story-card": editorial(),
        focused: props.focusedID === focusID(),
        read: cellReadVisuals().dimmed,
        "is-read": props.refined === true && cellReadVisuals().dimmed,
        pressed: props.pressed,
        "mobile-story-card": props.cell.mobileStoryCard === true,
        "mobile-tile-cell": props.cell.mobileTile === true,
        expanded: props.cell.headlineExpanded === true,
        "all-items-cell": props.readContext === "all-items",
        "no-media": !lead()?.media_url,
        "text-cell": !editorial() && !lead()?.media_url,
        "video-cell": !editorial() && lead()?.media_type === "video",
        "span-2": !editorial() && props.cell.span === 2,
        "tall-hero": !editorial() && props.cell.tall === true,
        "hero-cell": !editorial() && props.row.kind === "hero",
        "pair-cell": !editorial() && props.row.kind === "pair",
        "sub-cell":
          !editorial() && props.row.kind === "span" && props.cell.span !== 2,
        "compact-cell": !editorial() && props.row.kind === "compact",
        [`size-${props.story.size.toLowerCase()}`]: true,
      }}
      style={{
        left: `${props.cell.left}px`,
        top: `${props.cell.offsetY ?? 0}px`,
        width: `${props.cell.width}px`,
        height: `${cellHeight()}px`,
      }}
      data-signal={lead()?.signal ?? 0}
      data-signal-fresh={signalFresh() ? "" : undefined}
      data-item-id={focusID()}
      data-focus-id={focusID()}
      data-story-id={props.story.story_id}
      onFocus={(event) => focusLeadUnlessHeadline(event.target)}
      onMouseOver={(event) => focusLeadUnlessHeadline(event.target)}
      onPointerDown={pointerDown}
      onPointerMove={props.onLongPressMove}
      onPointerUp={props.onLongPressEnd}
      onPointerCancel={props.onLongPressEnd}
    >
      <Show when={lead()} keyed>
        {(item) => {
          const leadReadVisuals = createMemo(() =>
            readVisualState(props.readContext, item.read),
          );
          return (
            <Show
              when={editorial()}
              fallback={
                <>
                  <Show when={item.media_url}>
                    <ResponsiveImage
                      item={item}
                      sizes={props.cell.width}
                      alt=""
                      loading="eager"
                      deferUntilVisible
                      maxDimension={768}
                      width={item.media_w}
                      height={item.media_h}
                    />
                  </Show>
                  <div class="cell-scrim" />
                  <div class="cell-corner">
                    <SignalLabel value={item.signal} />
                    <UnreadDot visible={leadReadVisuals().unreadDot} />
                    <span class="cell-age">
                      {relativeTime(item.published_ts)}
                    </span>
                  </div>
                  <Show when={item.hearted}>
                    <span class="kept-marker" aria-hidden="true">
                      <Icon name="keep" size={14} filled={true} />
                    </span>
                  </Show>
                  <Show
                    when={
                      props.cell.mobileTile === true &&
                      props.story.items.length > 1
                    }
                    fallback={
                      <span
                        class="story-stack-badge"
                        role="img"
                        aria-label={`${props.story.source_count} sources`}
                      >
                        <Icon name="stack" size={13} />
                        <b>{props.story.source_count}</b>
                      </span>
                    }
                  >
                    <button
                      type="button"
                      class="story-stack-badge"
                      aria-label={`${props.story.source_count} sources, show headlines`}
                      aria-haspopup="dialog"
                      onPointerDown={(event) => event.stopPropagation()}
                      onClick={(event) => {
                        event.stopPropagation();
                        props.onMore(props.story);
                      }}
                    >
                      <Icon name="stack" size={13} />
                      <b>{props.story.source_count}</b>
                    </button>
                  </Show>
                  <PrimaryAction
                    item={item}
                    class="cell-main"
                    onFocus={() => props.onFocus(focusID())}
                    onOpen={() => props.onOpenLead(props.story)}
                    onExternalOpen={props.onExternalOpen}
                  />
                  <CellCopy
                    item={item}
                    refined={props.refined}
                    unreadDot={leadReadVisuals().unreadDot}
                    archive={false}
                    effectiveSize="M"
                    condensed={false}
                    explanation={whyText(item)}
                    story={true}
                    onUndo={() => props.onSignal(item, 0)}
                    dimmed={leadReadVisuals().dimmed}
                    onApplyFeed={() => props.onApplyFeed(item)}
                  />
                  <SignalActions
                    item={item}
                    size={props.story.size}
                    story={editorial()}
                    onSignal={props.onSignal}
                    onHeart={props.onHeart}
                    onMore={() => props.onMore(props.story)}
                  />
                </>
              }
            >
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
                        loading="eager"
                        deferUntilVisible
                        maxDimension={768}
                      />
                    </div>
                  </PrimaryAction>
                </Show>
                <Show when={props.refined && !item.signal}>
                  <span class="ranking-hint">
                    {props.story.source_count} sources
                    <Show when={whyText(item)}> · {whyText(item)}</Show>
                  </span>
                </Show>
                <div class="story-badges">
                  <span class="story-source-label">{sourceLabel()}</span>
                  <strong
                    class="story-source-count"
                    role="img"
                    aria-label={`${props.story.source_count} sources`}
                  >
                    <Icon name="stack" size={13} />
                    <b>{props.story.source_count}</b>
                  </strong>
                  <em>top 10%</em>
                </div>
                <SignalActions
                  item={item}
                  size={props.story.size}
                  story={editorial()}
                  onSignal={props.onSignal}
                  onHeart={props.onHeart}
                  onMore={() => props.onMore(props.story)}
                />
                <div class="story-corner cell-corner">
                  <SignalLabel value={item.signal} />
                  <UnreadDot visible={leadReadVisuals().unreadDot} />
                  <span>{relativeTime(item.published_ts)}</span>
                </div>
                <PrimaryAction
                  item={item}
                  class="story-lead"
                  onFocus={() => props.onFocus(focusID())}
                  onOpen={() => props.onOpenLead(props.story)}
                  onExternalOpen={props.onExternalOpen}
                >
                  <h2
                    ref={measureTitle}
                    classList={{ read: leadReadVisuals().dimmed }}
                  >
                    {headlineText(item.title)}
                  </h2>
                  <Show when={item.summary}>
                    <p
                      style={{ "--story-summary-lines": summaryLines() }}
                      classList={{
                        "story-summary-hidden": summaryLines() === 0,
                      }}
                    >
                      {item.summary}
                    </p>
                  </Show>
                  <Show when={!props.cell.mobileStoryCard || props.refined}>
                    <div class="story-meta">
                      <Show when={props.refined}>
                        <UnreadDot visible={leadReadVisuals().unreadDot} />
                      </Show>
                      <SourceBadge
                        connector={item.connector}
                        imageURL={item.favicon_url}
                        title={item.feed_title}
                        size={16}
                      />
                      <span>
                        {props.refined
                          ? gridSourceName(item)
                          : item.feed_title || "Feed"}
                      </span>
                      <small>· {relativeTime(item.published_ts)}</small>
                      <Show when={props.refined && item.read}>
                        <span class="refined-read-label">
                          <Icon name="check" size={13} /> read
                        </span>
                      </Show>
                      <Show when={!item.signal && whyText(item)}>
                        <em title={whyText(item)}>{whyText(item)}</em>
                      </Show>
                    </div>
                  </Show>
                </PrimaryAction>
                <Show when={item.signal !== 0 && !cellReadVisuals().dimmed}>
                  <div class="why-hint has-signal story-signal-why">
                    <SignalWhy
                      value={item.signal}
                      story={true}
                      onUndo={() => props.onSignal(item, 0)}
                    />
                  </div>
                </Show>
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
            </Show>
          );
        }}
      </Show>
      <Show when={editorial() && lead()?.hearted}>
        <span class="kept-marker" aria-hidden="true">
          <Icon name="keep" size={14} filled={true} />
        </span>
      </Show>
      <SignalMarker value={lead()?.signal ?? 0} />
      <Show when={editorial() && showHeadlines()}>
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
                    "related-also":
                      props.refined === true &&
                      repeatsLeadHeadline(lead().title, item.title),
                    "related-angle":
                      props.refined === true &&
                      !repeatsLeadHeadline(lead().title, item.title),
                  }}
                  data-focus-id={item.item_id}
                  onFocus={() => props.onFocus(item.item_id)}
                  onMouseEnter={() => props.onFocus(item.item_id)}
                  onOpen={() => props.onOpen(item)}
                  onExternalOpen={props.onExternalOpen}
                >
                  <Show
                    when={props.refined}
                    fallback={
                      <>
                        <span class="story-headline-dot">
                          <UnreadDot visible={readVisuals().unreadDot} />
                        </span>
                        <SourceBadge
                          connector={item.connector}
                          imageURL={item.favicon_url}
                          title={item.feed_title}
                          size={props.cell.mobileStoryCard ? 16 : 12}
                        />
                        <span class="story-headline-copy">
                          <span class="story-headline-feed">
                            {item.feed_title || "Feed"}
                            {"\u00a0\u00a0"}
                          </span>
                          <span class="story-headline-title">
                            {headlineText(item.title)}
                          </span>
                        </span>
                        <time>{relativeTime(item.published_ts)}</time>
                      </>
                    }
                  >
                    <RelatedCoverage
                      lead={lead()}
                      item={item}
                      age={relativeTime(item.published_ts)}
                    />
                  </Show>
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
          <Show when={props.cell.headlineExpanded === true}>
            <button
              type="button"
              class="story-more story-less"
              onClick={() => props.onExpand(props.story.story_id)}
            >
              Show less
              <Icon name="chevron-up" size={12} />
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
  children?: JSX.Element;
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
          aria-label={`Open ${headlineText(props.item.title)}`}
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
        aria-label={`Open ${headlineText(props.item.title)} on ${externalHost(props.item.external_url)}`}
        onFocus={props.onFocus}
        onMouseEnter={props.onMouseEnter}
        onClick={() => props.onExternalOpen(props.item)}
      >
        {props.children}
      </a>
    </Show>
  );
}
