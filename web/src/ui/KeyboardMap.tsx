import { For } from "solid-js";

const bindings = [
  ["H / ←", "previous cell"],
  ["L / →", "next cell"],
  ["J / ↓", "next row"],
  ["K / ↑", "previous row"],
  ["↵ / O", "open in reader"],
  ["N / P", "next / previous in reader"],
  ["+ / .", "thumbs up"],
  ["− / ,", "thumbs down"],
  ["F", "toggle heart"],
  ["M", "toggle read"],
  ["⇧ M", "mark all below read"],
  ["U", "undo last read batch"],
  ["V", "open original"],
  ["T", "toggle order"],
  ["A", "toggle all items"],
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
            ? to close
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
