// biome-ignore-all lint/a11y/useSemanticElements: The compact control contract uses button-based radio controls.
import { For, onCleanup, onMount } from "solid-js";
import { Portal } from "solid-js/web";
import { Icon } from "../components/Icon";
import { ITEM_WINDOWS, type ItemWindow } from "../item-view";
import { useSheetDrag } from "./use-sheet-drag";

export function FilterSheet(props: {
  window: ItemWindow;
  unreadOnly: boolean;
  counts: Partial<Record<ItemWindow, number>>;
  onWindow(window: ItemWindow): void;
  onUnreadOnly(next: boolean): void;
  onClose(): void;
}) {
  let panel: HTMLElement | undefined;
  let closeButton: HTMLButtonElement | undefined;
  const previousFocus = document.activeElement;
  const drag = useSheetDrag({
    panel: () => panel,
    onDismiss: props.onClose,
    scrollTop: () => panel?.scrollTop,
  });
  onMount(() => {
    closeButton?.focus();
    const keydown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopImmediatePropagation();
        props.onClose();
      } else if (event.key === "Tab") {
        const buttons = Array.from(
          panel?.querySelectorAll<HTMLButtonElement>("button") ?? [],
        );
        const first = buttons[0],
          last = buttons.at(-1);
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }
    };
    document.addEventListener("keydown", keydown, true);
    onCleanup(() => {
      document.removeEventListener("keydown", keydown, true);
      if (previousFocus instanceof HTMLElement && previousFocus.isConnected)
        previousFocus.focus({ preventScroll: true });
    });
  });
  return (
    <Portal>
      <div
        class="action-sheet-layer"
        role="presentation"
        onPointerDown={(event) => {
          if (event.target === event.currentTarget) props.onClose();
        }}
      >
        <div
          class="sheet-scrim-visual"
          aria-hidden="true"
          style={{ opacity: drag.scrimOpacity() }}
        />
        <section
          ref={panel}
          class="action-sheet filter-sheet"
          classList={{ "sheet-dragging": drag.dragging() }}
          role="dialog"
          aria-modal="true"
          aria-label="Filter"
          style={{ transform: `translateY(${drag.offset()}px)` }}
          onPointerDown={drag.onPointerDown}
          onPointerMove={drag.onPointerMove}
          onPointerUp={drag.onPointerUp}
          onPointerCancel={drag.onPointerCancel}
        >
          <i class="sheet-handle" aria-hidden="true" />
          <header>
            <strong>Filter</strong>
            <button
              ref={closeButton}
              type="button"
              aria-label="Close filter"
              onClick={props.onClose}
            >
              <Icon name="close" size={20} />
            </button>
          </header>
          <p class="filter-section-label">Dates</p>
          <div role="radiogroup" aria-label="Items shown">
            <For each={ITEM_WINDOWS}>
              {(option) => (
                <button
                  class="filter-date"
                  type="button"
                  role="radio"
                  aria-label={option.label}
                  aria-checked={props.window === option.value}
                  onClick={() => {
                    props.onWindow(option.value);
                    props.onClose();
                  }}
                >
                  <Icon name="check" size={18} />
                  <span>{option.label}</span>
                  <span class="scope-count">
                    {props.counts[option.value]?.toLocaleString("en-US") ?? "–"}
                  </span>
                </button>
              )}
            </For>
          </div>
          <p class="filter-section-label">Reading status</p>
          <button
            class="filter-unread"
            type="button"
            role="switch"
            aria-label="Unread only"
            aria-checked={props.unreadOnly}
            onClick={() => props.onUnreadOnly(!props.unreadOnly)}
          >
            <span>
              Unread only<small>Combines with any date</small>
            </span>
            <i class="filter-switch" aria-hidden="true" />
          </button>
        </section>
      </div>
    </Portal>
  );
}
