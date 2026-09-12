import { For, onCleanup, onMount } from "solid-js";
import { lightboxBindings } from "./lightbox-keys";

const groups = [
  {
    title: "Lightbox",
    bindings: lightboxBindings,
  },
  {
    title: "Navigation",
    bindings: [
      ["h / ← · l / →", "Previous / next cell"],
      ["j / ↓ · k / ↑", "Next / previous row"],
      ["Enter / o", "Open in reader"],
      ["n / j · p / k", "Next / previous item in reader"],
      ["Space / PageDown", "Page down"],
      ["Shift+Space / PageUp", "Page up"],
      ["Home / g → g", "Return to top"],
      ["End / Shift+g", "Go to caught-up card"],
    ],
  },
  {
    title: "Views",
    bindings: [
      ["g → u", "Show Unread"],
      ["g → t", "Show Today"],
      ["g → y", "Show Yesterday"],
      ["g → a", "Show All"],
      ["g → r", "Open Archive"],
      ["g → s", "Open / close Feeds & settings"],
      ["a", "Toggle Unread / All"],
      ["Shift+a", "Toggle Archive"],
      ["t", "Toggle Front page / Latest"],
    ],
  },
  {
    title: "Item actions",
    bindings: [
      ["+ / .", "Boost / undo boost"],
      ["− / ,", "Bury / undo bury"],
      ["f", "Keep / unkeep"],
      ["m", "Toggle read for the item or story (grid)"],
      ["Shift+F10 / Menu", "More actions for the item or story (grid)"],
      ["Shift+m", "Mark read from here on Front page; below here in Latest"],
      ["u", "Undo last read batch / grid clear"],
      ["c", "Copy or share original; Reddit discussion in grid"],
      ["v", "Open original"],
      ["r", "Show related coverage"],
    ],
  },
  {
    title: "General",
    bindings: [
      ["/", "Search"],
      ["#", "Filter by tag or feed"],
      ["Escape", "Close reader, dialog or active filter"],
      ["?", "Open / close keyboard help"],
    ],
  },
];

export function KeyboardMap(props: {
  onClose(): void;
  characterShortcuts: boolean;
  onCharacterShortcuts(enabled: boolean): void;
}) {
  let dialog!: HTMLElement;
  let closeButton!: HTMLButtonElement;
  onMount(() => {
    const previous = document.activeElement;
    closeButton.focus();
    onCleanup(() => {
      if (previous instanceof HTMLElement && previous.isConnected)
        previous.focus();
    });
  });
  return (
    <div class="modal-backdrop">
      <section
        ref={dialog}
        class="keys-card"
        onKeyDown={(event) => {
          if (event.isComposing) return;
          if (event.key === "Escape") {
            event.preventDefault();
            event.stopPropagation();
            props.onClose();
          } else if (event.key === "Tab") {
            const controls = Array.from(
              dialog.querySelectorAll<HTMLElement>("button, input"),
            );
            const first = controls[0];
            const last = controls.at(-1);
            if (event.shiftKey && document.activeElement === first) {
              event.preventDefault();
              last?.focus();
            } else if (!event.shiftKey && document.activeElement === last) {
              event.preventDefault();
              first?.focus();
            }
          }
        }}
        role="dialog"
        aria-modal="true"
        aria-labelledby="keys-title"
      >
        <header>
          <h2 id="keys-title">Keyboard</h2>
          <button ref={closeButton} type="button" onClick={props.onClose}>
            Close · Esc
          </button>
        </header>
        <p class="keys-note">
          Press sequences one key at a time: g → t means g, then t. Shortcuts
          pause while typing. Reader keys are suspended while the lightbox is
          open.
        </p>
        <label class="keys-preference">
          <input
            type="checkbox"
            checked={props.characterShortcuts}
            onChange={(event) =>
              props.onCharacterShortcuts(event.currentTarget.checked)
            }
          />
          Letter and symbol shortcuts
        </label>
        <div class="key-groups">
          <For each={groups}>
            {(group) => (
              <section aria-label={group.title}>
                <h3>{group.title}</h3>
                <div class="key-grid">
                  <For each={group.bindings}>
                    {([key, label]) => (
                      <div class="key-row">
                        <kbd>{key}</kbd>
                        <span>{label}</span>
                      </div>
                    )}
                  </For>
                </div>
              </section>
            )}
          </For>
        </div>
      </section>
    </div>
  );
}
