import { For } from "solid-js";

const bindings = [
  ["H / ←", "previous cell"],
  ["L / →", "next cell"],
  ["J / ↓", "next row"],
  ["K / ↑", "previous row"],
  ["↵ / O", "open in reader"],
  ["N / P", "next / previous in reader"],
  ["Space / PgDn / PgUp", "page through reader"],
  ["+ / .", "boost"],
  ["− / ,", "bury"],
  ["F", "toggle heart"],
  ["M", "marks the story read when a block is focused"],
  ["⇧ M", "marks stories below, then the grid read"],
  ["End / ⇧ G", "go to caught-up card"],
  ["Home / G G", "return to top"],
  ["U", "undo last read batch"],
  ["C", "copy or share original link"],
  ["V", "open original"],
  ["T", "toggle order"],
  ["A", "toggle all items"],
  ["⇧ A", "toggle archive"],
  ["G S", "feeds & settings"],
  ["ESC", "close reader"],
  ["?", "this map"],
];

export function KeyboardMap(props: { onClose(): void }) {
  return (
    <div class="modal-backdrop">
      <section
        class="keys-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby="keys-title"
      >
        <header>
          <h2 id="keys-title">Keyboard</h2>
          <button type="button" onClick={props.onClose}>
            ? / Esc to close
          </button>
        </header>
        <div class="key-grid">
          <For each={bindings}>
            {([key, label]) => (
              <div class="key-row">
                <kbd>{key}</kbd>
                <span>{label}</span>
              </div>
            )}
          </For>
        </div>
      </section>
    </div>
  );
}
