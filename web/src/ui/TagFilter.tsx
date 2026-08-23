import {
  createMemo,
  createSignal,
  For,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import type { Feed } from "../types";
import { feedTagOptions } from "./tag-options";

export function TagFilter(props: {
  feeds: Feed[];
  value: string;
  active: boolean;
  onChange(tag: string): void;
}) {
  const [open, setOpen] = createSignal(false);
  const [query, setQuery] = createSignal("");
  const [highlight, setHighlight] = createSignal(0);
  let input!: HTMLInputElement;

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
      clear();
    } else if (event.key === "Backspace" && !query()) {
      clear();
    }
  };

  return (
    <div class="grid-tag-filter">
      <Show
        when={open()}
        fallback={
          <button
            type="button"
            classList={{ active: Boolean(props.value) }}
            aria-label={
              props.value
                ? `Filter by #${props.value}; clear filter`
                : "Filter by tag"
            }
            onClick={() => (props.value ? clear() : begin())}
          >
            #{props.value || "all"}
            <Show when={props.value}> ×</Show>
          </button>
        }
      >
        <div class="tag-combobox">
          <span>#</span>
          <input
            ref={input}
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
                  aria-selected={index() === highlight()}
                  classList={{ highlighted: index() === highlight() }}
                  onMouseDown={(event) => event.preventDefault()}
                  onMouseEnter={() => setHighlight(index())}
                  onClick={() => apply(option.tag)}
                >
                  <span>#{option.tag}</span>
                  <small>{option.count}</small>
                </button>
              )}
            </For>
          </div>
        </div>
      </Show>
    </div>
  );
}
