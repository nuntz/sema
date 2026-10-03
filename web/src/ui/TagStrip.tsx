import { createEffect, For, on } from "solid-js";
import { Icon } from "../components/Icon";
import type { GridScope } from "../types";
import type { TagFilterMode } from "./TagFilter";
import type { TagStripChip } from "./tag-strip";

/**
 * One-tap tag hopping under the compact header. Hidden while the toolbar is
 * collapsed; the header keeps the active Scope visible meanwhile.
 */
export function TagStrip(props: {
  chips: TagStripChip[];
  scope: GridScope;
  collapsed: boolean;
  onTag(tag: string): void;
  onOpenPalette(mode: TagFilterMode): void;
}) {
  let scroller!: HTMLDivElement;
  createEffect(
    on(
      () => props.scope,
      () => scroller.scrollTo({ left: 0 }),
      { defer: true },
    ),
  );
  return (
    <div
      class="tag-strip"
      classList={{ "tag-strip--collapsed": props.collapsed }}
      aria-hidden={props.collapsed}
    >
      <div
        class="tag-strip__scroller"
        ref={scroller}
        role="toolbar"
        aria-label="Tags"
      >
        <button
          type="button"
          class="tag-strip__chip tag-strip__chip--hash"
          aria-label="Filter by tag or feed"
          tabIndex={props.collapsed ? -1 : 0}
          onClick={() => props.onOpenPalette("all")}
        >
          <Icon name="hash" size={16} />
        </button>
        <For each={props.chips}>
          {(chip) => (
            <button
              type="button"
              class="tag-strip__chip"
              aria-label={`Filter by tag ${chip.tag}, ${chip.count} items`}
              tabIndex={props.collapsed ? -1 : 0}
              onClick={() => props.onTag(chip.tag)}
            >
              <span>{chip.tag}</span>
              <small>{chip.count}</small>
            </button>
          )}
        </For>
        <button
          type="button"
          class="tag-strip__chip tag-strip__chip--feeds"
          aria-label="Filter by feed"
          tabIndex={props.collapsed ? -1 : 0}
          onClick={() => props.onOpenPalette("feeds")}
        >
          <span>Feeds</span>
          <Icon name="chevron-right" size={13} />
        </button>
      </div>
    </div>
  );
}
