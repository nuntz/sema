import {
  createEffect,
  createMemo,
  createSignal,
  For,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import { Icon } from "../components/Icon";
import { Tooltip } from "../components/Tooltip";
import type { Feed, FeedItemCounts, GridScope } from "../types";
import { SourceBadge } from "./SourceBadge";
import {
  feedScopeChip,
  optionScope,
  type ScopeFilterOption,
  scopeFilterOptions,
  scopeForClosedEscape,
  scopeForEnter,
  scopeOptionID,
} from "./tag-options";

export function TagFilter(props: {
  feeds: Feed[];
  itemCounts: FeedItemCounts;
  unreadOnly: boolean;
  value: GridScope;
  active: boolean;
  openRequest?: number;
  tooltipDisabled?: boolean;
  onOpenChange?(open: boolean): void;
  onChange(scope: GridScope): void;
}) {
  const [open, setOpen] = createSignal(false);
  const [query, setQuery] = createSignal("");
  const [highlight, setHighlight] = createSignal(0);
  let input!: HTMLInputElement;
  let previousOpenRequest = props.openRequest;

  const matches = createMemo(() =>
    scopeFilterOptions(
      props.feeds,
      query(),
      props.itemCounts,
      props.unreadOnly,
    ),
  );
  const tagMatches = createMemo(() =>
    matches().filter(
      (option): option is Extract<ScopeFilterOption, { kind: "tag" }> =>
        option.kind === "tag",
    ),
  );
  const feedMatches = createMemo(() =>
    matches().filter(
      (option): option is Extract<ScopeFilterOption, { kind: "feed" }> =>
        option.kind === "feed",
    ),
  );
  const activeFeed = createMemo(() => feedScopeChip(props.value, props.feeds));

  const begin = () => {
    setQuery("");
    setHighlight(0);
    setOpen(true);
    queueMicrotask(() => input?.focus());
  };
  const apply = (scope: GridScope) => {
    props.onChange(scope);
    setOpen(false);
    setQuery("");
  };
  const clear = () => apply(null);

  createEffect(() => props.onOpenChange?.(open()));
  createEffect(() => {
    const request = props.openRequest;
    if (request !== undefined && request !== previousOpenRequest) begin();
    previousOpenRequest = request;
  });

  onMount(() => {
    const keydown = (event: KeyboardEvent) => {
      if (
        !props.active ||
        event.repeat ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey
      )
        return;
      const target = event.target;
      if (
        target instanceof HTMLElement &&
        (target.isContentEditable || target.matches("input, textarea, select"))
      )
        return;
      if (event.key === "#") {
        event.preventDefault();
        begin();
      } else if (event.key === "Escape" && !open()) {
        const next = scopeForClosedEscape(props.value);
        if (next !== undefined) {
          event.preventDefault();
          apply(next);
        }
      }
    };
    window.addEventListener("keydown", keydown);
    onCleanup(() => window.removeEventListener("keydown", keydown));
  });

  const onKeyDown = (event: KeyboardEvent) => {
    const available = matches();
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setHighlight((value) => (value + 1) % Math.max(available.length, 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setHighlight(
        (value) =>
          (value - 1 + Math.max(available.length, 1)) %
          Math.max(available.length, 1),
      );
    } else if (event.key === "Enter") {
      event.preventDefault();
      const next = scopeForEnter(available, highlight());
      if (next) apply(next);
    } else if (event.key === "Tab") {
      const option = available[highlight()] ?? available[0];
      if (option) {
        event.preventDefault();
        setQuery(option.label);
      }
    } else if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      setOpen(false);
      setQuery("");
      input?.blur();
    } else if (event.key === "Backspace" && !query()) {
      clear();
    }
  };

  return (
    <div class="grid-tag-filter" classList={{ "is-open": open() }}>
      <Show
        when={open()}
        fallback={
          <Show
            when={props.value}
            fallback={
              <Tooltip
                name="Filter by tag or feed"
                shortcut="#"
                disabled={props.tooltipDisabled}
              >
                <button
                  type="button"
                  class="chrome-icon header-icon-button tag-trigger"
                  aria-label="Filter by tag or feed"
                  onClick={begin}
                >
                  <Icon name="tag" size={18} />
                </button>
              </Tooltip>
            }
          >
            {(scope) => (
              <Show
                when={scope().kind === "feed"}
                fallback={
                  <Tooltip
                    name={`Clear tag filter: #${scope().value}`}
                    disabled={props.tooltipDisabled}
                  >
                    <button
                      type="button"
                      class="active-tag-chip"
                      aria-label={`Clear tag filter: #${scope().value}`}
                      onClick={clear}
                    >
                      <span class="tag-chip-hash">#</span>
                      <span class="tag-chip-name">{scope().value}</span>
                      <span class="tag-chip-close" aria-hidden="true">
                        <Icon name="close" size={13} />
                      </span>
                    </button>
                  </Tooltip>
                }
              >
                <Tooltip
                  name={activeFeed()?.ariaLabel ?? "Clear feed filter"}
                  disabled={props.tooltipDisabled}
                >
                  <button
                    type="button"
                    class="active-tag-chip active-feed-chip"
                    aria-label={activeFeed()?.ariaLabel ?? "Clear feed filter"}
                    onClick={clear}
                  >
                    <SourceBadge
                      connector={activeFeed()?.option?.connector}
                      imageURL={activeFeed()?.option?.faviconURL}
                      title={activeFeed()?.title ?? scope().value}
                      size={16}
                    />
                    <span class="tag-chip-name">
                      {activeFeed()?.title ?? scope().value}
                    </span>
                    <span class="tag-chip-close" aria-hidden="true">
                      <Icon name="close" size={13} />
                    </span>
                  </button>
                </Tooltip>
              </Show>
            )}
          </Show>
        }
      >
        <div class="tag-combobox">
          <span>#</span>
          <input
            ref={input}
            aria-label="Filter by tag or feed"
            role="combobox"
            aria-expanded="true"
            aria-controls="grid-scope-options"
            aria-activedescendant={
              matches()[highlight()]
                ? scopeOptionID(matches()[highlight()])
                : undefined
            }
            value={query()}
            onInput={(event) => {
              setQuery(event.currentTarget.value);
              setHighlight(0);
            }}
            onKeyDown={onKeyDown}
            onBlur={(event) => {
              if (
                !event.currentTarget.parentElement?.parentElement?.contains(
                  event.relatedTarget as Node,
                )
              )
                setOpen(false);
            }}
          />
          <div id="grid-scope-options" class="grid-tag-menu" role="listbox">
            <Show
              when={matches().length > 0}
              fallback={<span class="no-tag">no matching scope</span>}
            >
              <Show when={tagMatches().length > 0}>
                <span class="grid-scope-heading">Tags</span>
                <For each={tagMatches()}>
                  {(option) => {
                    const index = () => matches().indexOf(option);
                    return (
                      <button
                        id={scopeOptionID(option)}
                        type="button"
                        role="option"
                        aria-selected={index() === highlight()}
                        class="tag-scope-option"
                        classList={{ highlighted: index() === highlight() }}
                        onMouseDown={(event) => event.preventDefault()}
                        onMouseEnter={() => setHighlight(index())}
                        onClick={() => apply(optionScope(option))}
                      >
                        <span>{option.tag}</span>
                        <small
                          title={
                            props.unreadOnly
                              ? `${option.count} unread items`
                              : `${option.count} items in the current window`
                          }
                        >
                          {option.count}
                        </small>
                      </button>
                    );
                  }}
                </For>
              </Show>
              <Show when={feedMatches().length > 0}>
                <span class="grid-scope-heading feed-scope-heading">Feeds</span>
                <For each={feedMatches()}>
                  {(option) => {
                    const index = () => matches().indexOf(option);
                    return (
                      <button
                        id={scopeOptionID(option)}
                        type="button"
                        role="option"
                        aria-selected={index() === highlight()}
                        class="feed-scope-option"
                        classList={{ highlighted: index() === highlight() }}
                        onMouseDown={(event) => event.preventDefault()}
                        onMouseEnter={() => setHighlight(index())}
                        onClick={() => apply(optionScope(option))}
                      >
                        <SourceBadge
                          connector={option.connector}
                          imageURL={option.faviconURL}
                          title={option.title}
                          size={16}
                        />
                        <span>{option.title}</span>
                        <small
                          title={
                            props.unreadOnly
                              ? `${option.count} unread items`
                              : `${option.count} items in the current window`
                          }
                        >
                          {option.count}
                        </small>
                      </button>
                    );
                  }}
                </For>
              </Show>
            </Show>
          </div>
        </div>
      </Show>
    </div>
  );
}
