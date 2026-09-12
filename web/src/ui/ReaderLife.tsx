import { For, Show } from "solid-js";
import { expiryLabel, expirySentence, hoursLeft } from "../expiry";
import { readerDay } from "../reader-expiry";
import { ExpiryPill } from "./ExpiryPill";

export function ReaderLife(props: { published: string; now: number }) {
  const day = () => readerDay(props.published, props.now);
  const urgent = () => hoursLeft(props.published, props.now) < 6;
  return (
    <span class="reader-life">
      <span
        class="reader-day-track"
        classList={{ "reader-day-track--urgent": urgent() }}
        role="img"
        aria-label={
          urgent()
            ? expirySentence(props.published, props.now)
            : `day ${day()} of 7`
        }
      >
        <span class="reader-day-squares" aria-hidden="true">
          <For each={[1, 2, 3, 4, 5, 6, 7]}>
            {(number) => (
              <i
                classList={{ spent: number < day(), current: number === day() }}
              />
            )}
          </For>
        </span>
        <span class="reader-day-label">
          {urgent()
            ? expiryLabel(hoursLeft(props.published, props.now))
            : `day ${day()} of 7`}
        </span>
      </span>
      <Show when={hoursLeft(props.published, props.now) <= 48}>
        <ExpiryPill labelled published={props.published} now={props.now} />
      </Show>
    </span>
  );
}
