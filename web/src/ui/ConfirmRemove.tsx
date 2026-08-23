import { onCleanup, onMount } from "solid-js";

export function ConfirmRemove(props: { onCancel(): void; onConfirm(): void }) {
  let remove!: HTMLButtonElement;
  let cancel!: HTMLButtonElement;
  const previous = document.activeElement;

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      props.onCancel();
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      event.stopPropagation();
      props.onConfirm();
      return;
    }
    if (event.key !== "Tab") return;
    event.preventDefault();
    (document.activeElement === remove ? cancel : remove).focus();
  };

  onMount(() => {
    remove.focus();
    window.addEventListener("keydown", onKeyDown, true);
    onCleanup(() => {
      window.removeEventListener("keydown", onKeyDown, true);
      if (previous instanceof HTMLElement && previous.isConnected)
        previous.focus();
    });
  });

  return (
    <div class="confirm-scrim">
      <section
        class="confirm-card"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="remove-title"
        aria-describedby="remove-description"
      >
        <h2 id="remove-title">Remove from archive?</h2>
        <p id="remove-description">The original may no longer be available.</p>
        <div>
          <button ref={cancel} type="button" onClick={props.onCancel}>
            Cancel
          </button>
          <button
            ref={remove}
            type="button"
            class="remove"
            onClick={props.onConfirm}
          >
            Remove
          </button>
        </div>
      </section>
    </div>
  );
}
