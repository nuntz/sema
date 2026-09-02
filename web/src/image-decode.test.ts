import { afterEach, describe, expect, it, vi } from "vitest";
import { decodeImageWithin } from "./image-decode";

afterEach(() => {
  vi.useRealTimers();
});

describe("decodeImageWithin", () => {
  it("reports a completed decode", async () => {
    await expect(
      decodeImageWithin({ decode: () => Promise.resolve() }, 4_000),
    ).resolves.toBe("decoded");
  });

  it("reports a decoder rejection", async () => {
    await expect(
      decodeImageWithin(
        { decode: () => Promise.reject(new Error("decode failed")) },
        4_000,
      ),
    ).resolves.toBe("error");
  });

  it("times out a decoder that remains pending", async () => {
    vi.useFakeTimers();
    const result = decodeImageWithin(
      { decode: () => new Promise(() => undefined) },
      4_000,
    );

    await vi.advanceTimersByTimeAsync(4_000);

    await expect(result).resolves.toBe("timeout");
  });

  it("treats browsers without decode support as ready", async () => {
    await expect(decodeImageWithin({}, 4_000)).resolves.toBe("decoded");
  });
});
