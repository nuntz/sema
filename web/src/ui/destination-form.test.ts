import { describe, expect, it } from "vitest";
import {
  type DestinationForm,
  destinationInput,
  destinationStatus,
  generateSecret,
  pingMessage,
  validateDestination,
} from "./destination-form";

const form = (patch: Partial<DestinationForm> = {}): DestinationForm => ({
  url: "https://receiver.example/hook",
  label: "Send",
  enabled: true,
  secret: "",
  secretSet: true,
  ...patch,
});

describe("destination form", () => {
  it.each([
    [form(), undefined],
    [form({ url: "http://receiver.example/hook" }), "Use an https:// URL"],
    [
      form({ url: "https://user:pw@receiver.example/hook" }),
      "Use an https:// URL",
    ],
    [form({ url: "not a url" }), "Use an https:// URL"],
    [form({ secretSet: false }), "Enter or generate a secret"],
    [form({ secret: "short" }), "Secrets need 32 to 256 characters"],
    [form({ label: "x".repeat(25) }), "Keep the label to 24 characters"],
  ])("validates %o", (value, error) => {
    expect(validateDestination(value)).toBe(error);
  });

  it("sends a new secret only when one was entered", () => {
    expect(
      destinationInput(
        form({ url: " https://receiver.example/hook ", label: " " }),
      ),
    ).toEqual({
      url: "https://receiver.example/hook",
      label: "Send",
      enabled: true,
    });
    expect(
      destinationInput(form({ secret: "s".repeat(32), label: "Save it" })),
    ).toEqual({
      url: "https://receiver.example/hook",
      label: "Save it",
      enabled: true,
      secret: "s".repeat(32),
    });
  });

  it("generates 32 random bytes as hex", () => {
    const secret = generateSecret((bytes) => {
      bytes.forEach((_, index) => {
        bytes[index] = index;
      });
      return bytes;
    });
    expect(secret).toBe(
      "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
    );
  });

  it("describes the last delivery", () => {
    const now = Date.parse("2026-09-26T18:10:00Z");
    expect(destinationStatus(undefined, now)).toBe("No deliveries yet");
    expect(
      destinationStatus(
        {
          last_status: "202",
          last_delivery_at: "2026-09-26T18:07:00.000000000Z",
        },
        now,
      ),
    ).toBe("Last delivery: 202 · 3 min ago");
    expect(
      destinationStatus(
        {
          last_status: "timeout",
          last_delivery_at: "2026-09-26T18:09:50.000000000Z",
        },
        now,
      ),
    ).toBe("Last delivery: timeout · just now");
    expect(
      destinationStatus(
        {
          last_status: "cancelled",
          last_delivery_at: "2026-09-26T15:00:00.000000000Z",
        },
        now,
      ),
    ).toBe("Last delivery: cancelled · 3 h ago");
  });

  it.each([
    [{ outcome: "sent", status: 200 }, "Test delivered: 200"],
    [{ outcome: "failed", status: 401 }, "Test refused: 401"],
    [
      { outcome: "failed", reason: "blocked address" },
      "Test failed: blocked address",
    ],
  ] as const)("describes the ping result %o", (result, message) => {
    expect(pingMessage(result)).toBe(message);
  });
});
