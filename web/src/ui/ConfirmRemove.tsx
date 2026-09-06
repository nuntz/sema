import { onCleanup, onMount } from "solid-js";
import { useSheetDrag } from "./use-sheet-drag";

export function ConfirmRemove(props: { onCancel(): void; onConfirm(): void }) {
  let remove!: HTMLButtonElement;
  let cancel!: HTMLButtonElement;
  let card!: HTMLElement;
  const previous = document.activeElement;
  const sheetDrag = useSheetDrag({
    panel: () => card,
    enabled: () => window.matchMedia("(max-width: 700px)").matches,
    onDismiss: props.onCancel,
  });

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
      <div
        class="confirm-scrim-visual"
        aria-hidden="true"
        style={{ opacity: sheetDrag.scrimOpacity() }}
      />
      <section
        ref={card}
        class="confirm-card"
        classList={{ "sheet-dragging": sheetDrag.dragging() }}
        style={{ transform: `translateY(${sheetDrag.offset()}px)` }}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="remove-title"
        aria-describedby="remove-description"
        onPointerDown={sheetDrag.onPointerDown}
        onPointerMove={sheetDrag.onPointerMove}
        onPointerUp={sheetDrag.onPointerUp}
        onPointerCancel={sheetDrag.onPointerCancel}
      >
        <i class="confirm-grabber" aria-hidden="true" />
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
