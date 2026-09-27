import { createEffect, createResource, createSignal, Show } from "solid-js";
import type { AppAPI } from "../api/client";
import { useClock } from "../clock";
import type { Destination } from "../types";
import {
  type DestinationForm,
  destinationInput,
  destinationStatus,
  generateSecret,
  pingMessage,
  validateDestination,
} from "./destination-form";

const emptyForm = (): DestinationForm => ({
  url: "",
  label: "Send",
  enabled: true,
  secret: "",
  secretSet: false,
});

const formFor = (destination: Destination | null): DestinationForm =>
  destination
    ? {
        url: destination.url,
        label: destination.label,
        enabled: destination.enabled,
        secret: "",
        secretSet: destination.secret_set,
      }
    : emptyForm();

export function DestinationSettings(props: {
  api: AppAPI;
  onSendLabel(label: string | null): void;
  onToast(kind: "success" | "error", message: string): void;
}) {
  const now = useClock();
  const [saved, { mutate: setSaved, refetch }] = createResource(() =>
    props.api.destination(),
  );
  const [form, setForm] = createSignal<DestinationForm>(emptyForm());
  const [reveal, setReveal] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [message, setMessage] = createSignal("");
  // Seed the form once from the stored Destination; later refetches only
  // refresh the delivery status line.
  let seeded = false;
  createEffect(() => {
    if (seeded || saved.state !== "ready") return;
    seeded = true;
    setForm(formFor(saved() ?? null));
  });
  const current = form;
  const update = (patch: Partial<DestinationForm>) =>
    setForm({ ...current(), ...patch });
  const publishLabel = (destination: Destination | null) =>
    props.onSendLabel(
      destination?.enabled ? destination.label || "Send" : null,
    );

  const save = async (event: SubmitEvent) => {
    event.preventDefault();
    const error = validateDestination(current());
    if (error) {
      setMessage(error);
      return;
    }
    setBusy(true);
    setMessage("");
    try {
      const destination = await props.api.saveDestination(
        destinationInput(current()),
      );
      setSaved(destination);
      setForm(formFor(destination));
      setReveal(false);
      publishLabel(destination);
      props.onToast("success", "Send settings saved");
    } catch (caught) {
      setMessage(caught instanceof Error ? caught.message : "Couldn't save");
    } finally {
      setBusy(false);
    }
  };

  const test = async () => {
    setBusy(true);
    setMessage("");
    try {
      setMessage(pingMessage(await props.api.pingDestination()));
      void refetch();
    } catch (caught) {
      setMessage(
        caught instanceof Error ? caught.message : "Couldn't send the test",
      );
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    setBusy(true);
    setMessage("");
    try {
      await props.api.deleteDestination();
      setSaved(null);
      setForm(emptyForm());
      publishLabel(null);
      props.onToast("success", "Send removed");
    } catch (caught) {
      setMessage(caught instanceof Error ? caught.message : "Couldn't remove");
    } finally {
      setBusy(false);
    }
  };

  return (
    <section class="destination-settings" aria-label="Send to settings">
      <h2>Send to…</h2>
      <p>
        Send one item at a time to an address you choose. Sema signs each
        request with your secret so the receiver can check it came from you.
      </p>
      <form onSubmit={(event) => void save(event)}>
        <label class="destination-toggle">
          <input
            type="checkbox"
            checked={current().enabled}
            onChange={(event) =>
              update({ enabled: event.currentTarget.checked })
            }
          />
          Show the Send action
        </label>
        <label class="destination-field">
          <span>URL</span>
          <input
            type="url"
            inputmode="url"
            autocomplete="off"
            spellcheck={false}
            placeholder="https://"
            value={current().url}
            onInput={(event) => update({ url: event.currentTarget.value })}
          />
        </label>
        <div class="destination-field">
          <label for="destination-secret">Secret</label>
          <div class="destination-secret">
            <input
              id="destination-secret"
              type={reveal() ? "text" : "password"}
              autocomplete="new-password"
              spellcheck={false}
              placeholder={
                current().secretSet
                  ? "Saved · enter a new one to replace it"
                  : "32 characters or more"
              }
              value={current().secret}
              onInput={(event) => {
                setReveal(false);
                update({ secret: event.currentTarget.value });
              }}
            />
            <button
              type="button"
              onClick={() => {
                update({ secret: generateSecret() });
                setReveal(true);
              }}
            >
              Generate
            </button>
            <Show when={current().secret}>
              <button type="button" onClick={() => setReveal(!reveal())}>
                {reveal() ? "Hide" : "Reveal"}
              </button>
            </Show>
          </div>
          <Show when={current().secret && reveal()}>
            <small>Copy this now. After saving it can only be replaced.</small>
          </Show>
        </div>
        <label class="destination-field">
          <span>Button label</span>
          <input
            type="text"
            maxlength={24}
            value={current().label}
            onInput={(event) => update({ label: event.currentTarget.value })}
          />
        </label>
        <Show when={saved()}>
          {(destination) => (
            <p class="destination-status">
              {destinationStatus(destination(), now())}
            </p>
          )}
        </Show>
        <div class="destination-actions">
          <button type="submit" disabled={busy()} aria-busy={busy()}>
            Save
          </button>
          <Show when={saved()}>
            <button type="button" disabled={busy()} onClick={() => void test()}>
              Send test
            </button>
            <button
              type="button"
              disabled={busy()}
              onClick={() => void remove()}
            >
              Remove
            </button>
          </Show>
        </div>
        <Show when={message()}>
          <p class="form-message" role="status">
            {message()}
          </p>
        </Show>
      </form>
    </section>
  );
}
