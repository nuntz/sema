import type { Destination, DestinationInput, SendResult } from "../types";

export interface DestinationForm {
  url: string;
  label: string;
  enabled: boolean;
  /** A newly entered or generated secret; empty keeps the stored one. */
  secret: string;
  secretSet: boolean;
}

const DEFAULT_LABEL = "Send";

function validURL(raw: string): boolean {
  try {
    const url = new URL(raw.trim());
    return (
      url.protocol === "https:" &&
      url.hostname !== "" &&
      url.username === "" &&
      url.password === ""
    );
  } catch {
    return false;
  }
}

export function validateDestination(form: DestinationForm): string | undefined {
  if (!validURL(form.url)) return "Use an https:// URL";
  if (!form.secret && !form.secretSet) return "Enter or generate a secret";
  if (form.secret && (form.secret.length < 32 || form.secret.length > 256))
    return "Secrets need 32 to 256 characters";
  if ([...form.label.trim()].length > 24)
    return "Keep the label to 24 characters";
}

export function destinationInput(form: DestinationForm): DestinationInput {
  const input: DestinationInput = {
    url: form.url.trim(),
    label: form.label.trim() || DEFAULT_LABEL,
    enabled: form.enabled,
  };
  if (form.secret) input.secret = form.secret;
  return input;
}

export function generateSecret(
  fill: (bytes: Uint8Array) => Uint8Array = (bytes) =>
    crypto.getRandomValues(bytes),
): string {
  return Array.from(fill(new Uint8Array(32)), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

export function destinationStatus(
  destination:
    | Pick<Destination, "last_status" | "last_delivery_at">
    | undefined,
  now: number,
): string {
  if (!destination?.last_status || !destination.last_delivery_at)
    return "No deliveries yet";
  const minutes = Math.floor(
    (now - Date.parse(destination.last_delivery_at)) / 60_000,
  );
  const age =
    minutes < 1
      ? "just now"
      : minutes < 60
        ? `${minutes} min ago`
        : minutes < 48 * 60
          ? `${Math.floor(minutes / 60)} h ago`
          : `${Math.floor(minutes / (24 * 60))} d ago`;
  return `Last delivery: ${destination.last_status} · ${age}`;
}

export function pingMessage(result: SendResult): string {
  if (result.outcome === "sent") return `Test delivered: ${result.status}`;
  if (result.status) return `Test refused: ${result.status}`;
  return `Test failed: ${result.reason ?? "unknown error"}`;
}
