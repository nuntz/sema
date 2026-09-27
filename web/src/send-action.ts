import { APIError } from "./api/client";
import type { SendResult } from "./types";

export interface SendToast {
  kind: "success" | "info" | "error";
  message: string;
}

// Errors name the Destination, not the button label, so a receiver's error
// never reads as Sema refusing the Send.
export function sendToast(result: SendResult): SendToast {
  switch (result.outcome) {
    case "sent":
      return { kind: "success", message: "Sent" };
    case "queued":
      return { kind: "info", message: "No answer yet, retrying" };
    default:
      return result.status
        ? { kind: "error", message: `Destination answered ${result.status}` }
        : {
            kind: "error",
            message: `Couldn't reach the Destination: ${result.reason ?? "unknown error"}`,
          };
  }
}

export function sendFailureToast(error: unknown): SendToast {
  if (error instanceof APIError && error.status === 429)
    return { kind: "error", message: "Too many sends, try later" };
  if (error instanceof APIError && error.status === 409)
    return { kind: "error", message: "Set up Send in Settings" };
  if (error instanceof APIError && error.status === 422)
    return { kind: "error", message: "This item is too large to send" };
  return { kind: "error", message: "Couldn't send" };
}
