import { describe, expect, it } from "vitest";
import { APIError } from "./api/client";
import { sendFailureToast, sendToast } from "./send-action";

describe("send toasts", () => {
  it.each([
    [{ outcome: "sent", status: 202 }, "success", "Sent"],
    [{ outcome: "queued", status: 503 }, "info", "No answer yet, retrying"],
    [{ outcome: "failed", status: 404 }, "error", "Destination answered 404"],
    [
      { outcome: "failed", reason: "blocked address" },
      "error",
      "Couldn't reach the Destination: blocked address",
    ],
  ] as const)("describes %o", (result, kind, message) => {
    expect(sendToast(result)).toEqual({ kind, message });
  });

  it.each([
    [new APIError("too many sends", 429), "Too many sends, try later"],
    [new APIError("no destination", 409), "Set up Send in Settings"],
    [
      new APIError("item is too large to send", 422),
      "This item is too large to send",
    ],
    [new Error("offline"), "Couldn't send"],
  ])("describes request failure %o", (error, message) => {
    expect(sendFailureToast(error)).toEqual({ kind: "error", message });
  });
});
