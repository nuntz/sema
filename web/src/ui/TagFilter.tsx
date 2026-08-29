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
import type { Feed } from "../types";
import { feedTagOptions } from "./tag-options";

export function TagFilter(props: {
  feeds: Feed[];
  value: string;
  active: boolean;
  openRequest?: number;
  tooltipDisabled?: boolean;
  onOpenChange?(open: boolean): void;
  onChange(tag: string): void;
}) {
  const [open, setOpen] = createSignal(false);
  const [query, setQuery] = createSignal("");
  const [highlight, setHighlight] = createSignal(0);
  let input!: HTMLInputElement;
  let previousOpenRequest = props.openRequest;

  const options = createMemo(() => feedTagOptions(props.feeds));
  const matches = createMemo(() => {
    const needle = query().trim().toLowerCase().replace(/^#/, "");
    return options().filter((option) => !needle || option.tag.includes(needle));
  });

  const begin = () => {
    setQuery("");
    setHighlight(0);
    setOpen(true);
    queueMicrotask(() => input?.focus());
  };
  const apply = (tag: string) => {
    props.onChange(tag);
    setOpen(false);
    setQuery("");
  };
  const clear = () => apply("");

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
      } else if (event.key === "Escape" && props.value && !open()) {
        event.preventDefault();
        clear();
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
      const option = available[highlight()] ?? available[0];
      if (option) apply(option.tag);
    } else if (event.key === "Tab") {
      const option = available[highlight()] ?? available[0];
      if (option) {
        event.preventDefault();
        setQuery(option.tag);
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
                name="Filter by tag"
                shortcut="#"
                disabled={props.tooltipDisabled}
              >
                <button
                  type="button"
                  class="chrome-icon header-icon-button tag-trigger"
                  aria-label="Filter by tag"
                  onClick={begin}
                >
                  <Icon name="tag" size={18} />
                </button>
              </Tooltip>
            }
          >
            {(value) => {
              const label = () => `Clear tag filter: #${value()}`;
              return (
                <Tooltip name={label()} disabled={props.tooltipDisabled}>
                  <button
                    type="button"
                    class="active-tag-chip"
                    aria-label={label()}
                    onClick={clear}
                  >
                    <span class="tag-chip-hash">#</span>
                    <span class="tag-chip-name">{value()}</span>
                    <span class="tag-chip-close" aria-hidden="true">
                      <Icon name="close" size={13} />
                    </span>
                  </button>
                </Tooltip>
              );
            }}
          </Show>
        }
      >
        <div class="tag-combobox">
          <span>#</span>
          <input
            ref={input}
            aria-label="Filter by tag"
            role="combobox"
            aria-expanded="true"
            aria-controls="grid-tag-options"
            aria-activedescendant={
              matches()[highlight()]
                ? `grid-tag-${matches()[highlight()].tag}`
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
          <div id="grid-tag-options" class="grid-tag-menu" role="listbox">
            <For each={matches()} fallback={<span class="no-tag">no tag</span>}>
              {(option, index) => (
                <button
                  id={`grid-tag-${option.tag}`}
                  type="button"
                  role="option"
                  classList={{ highlighted: index() === highlight() }}
                  onMouseDown={(event) => event.preventDefault()}
                  onMouseEnter={() => setHighlight(index())}
                  onClick={() => apply(option.tag)}
                >
                  <span>{option.tag}</span>
                  <small title={`${option.count} items ingested`}>
                    {option.count}
                  </small>
                </button>
              )}
            </For>
          </div>
        </div>
      </Show>
    </div>
  );
}
