import {
  expiryLabel,
  expiryRingFraction,
  expirySentence,
  expiryState,
  hoursLeft,
} from "../expiry";

export function ExpiryPill(props: {
  published: string;
  now: number;
  compact?: boolean;
  unread?: boolean;
  labelled?: boolean;
}) {
  const hours = () => hoursLeft(props.published, props.now);
  return (
    // biome-ignore lint/a11y/useAriaPropsSupportedByRole: Only labelled reader pills have the image role and accessible name.
    <span
      role={props.labelled ? "img" : undefined}
      aria-hidden={!props.labelled}
      class={`expiry-pill expiry-pill--${expiryState(hours())}`}
      title={expirySentence(props.published, props.now)}
      aria-label={
        props.labelled ? expirySentence(props.published, props.now) : undefined
      }
    >
      <svg width="11" height="11" viewBox="0 0 12 12" aria-hidden="true">
        <circle
          class="expiry-ring-track"
          classList={{
            "expiry-ring-track--unread": props.compact && props.unread,
          }}
          cx="6"
          cy="6"
          r="4.4"
          fill="none"
          stroke-width="2.2"
        />
        <circle
          cx="6"
          cy="6"
          r="4.4"
          fill="none"
          stroke="currentColor"
          stroke-width="2.2"
          transform="rotate(-90 6 6)"
          stroke-dasharray={`${expiryRingFraction(hours()) * 28.27} 28.27`}
        />
      </svg>
      {expiryLabel(hours(), { compact: props.compact })}
    </span>
  );
}
