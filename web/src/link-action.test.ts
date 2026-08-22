import { describe, expect, it, vi } from "vitest";
import {
  copyOriginalLink,
  isCancelledShare,
  LinkActionFailure,
} from "./link-action";

const target = { url: "https://example.com/article", title: "Article" };

describe("copy original link", () => {
  it("copies on non-touch devices even when sharing is available", async () => {
    const share = vi.fn(async () => {});
    const writeText = vi.fn(async () => {});

    await expect(
      copyOriginalLink(target, { touch: false, share, writeText }),
    ).resolves.toBe("copied");
    expect(writeText).toHaveBeenCalledWith(target.url);
    expect(share).not.toHaveBeenCalled();
  });

  it("uses Web Share on touch devices", async () => {
    const share = vi.fn(async () => {});
    const writeText = vi.fn(async () => {});

    await expect(
      copyOriginalLink(target, { touch: true, share, writeText }),
    ).resolves.toBe("shared");
    expect(share).toHaveBeenCalledWith(target);
    expect(writeText).not.toHaveBeenCalled();
  });

  it("falls back to copying when Web Share is unavailable", async () => {
    const writeText = vi.fn(async () => {});
    await expect(
      copyOriginalLink(target, { touch: true, writeText }),
    ).resolves.toBe("copied");
  });

  it("recognizes a cancelled share", async () => {
    const aborted = new Error("cancelled");
    aborted.name = "AbortError";
    const share = vi.fn(async () => {
      throw aborted;
    });

    const error = await copyOriginalLink(target, { touch: true, share }).catch(
      (caught) => caught,
    );
    expect(error).toBeInstanceOf(LinkActionFailure);
    expect(isCancelledShare(error)).toBe(true);
  });
});
