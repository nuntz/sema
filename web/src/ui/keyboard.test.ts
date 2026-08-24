import { describe, expect, it } from "vitest";
import { appCommand, gridCommand, readerCommand } from "./keyboard";

describe("keyboard map", () => {
  it.each([
    ["j", "down"],
    ["k", "up"],
    ["h", "left"],
    ["l", "right"],
    ["ArrowDown", "down"],
    ["ArrowUp", "up"],
    ["ArrowLeft", "left"],
    ["ArrowRight", "right"],
    ["Enter", "open"],
    ["o", "open"],
    ["+", "like"],
    [".", "like"],
    ["-", "dislike"],
    [",", "dislike"],
    ["f", "heart"],
    ["m", "read"],
    ["M", "mark-below"],
    ["End", "end"],
    ["G", "end"],
    ["Home", "home"],
    ["g", "go-prefix"],
    ["u", "undo"],
    ["c", "copy"],
    ["v", "original"],
    ["t", "order"],
    ["r", "related"],
  ])("maps grid key %s", (key, command) =>
    expect(gridCommand(key)).toBe(command),
  );

  it.each([
    ["Escape", "close"],
    ["n", "next"],
    ["p", "previous"],
    ["+", "like"],
    ["-", "dislike"],
    ["f", "heart"],
    ["c", "copy"],
    ["v", "original"],
    ["r", "related"],
  ])("maps reader key %s", (key, command) =>
    expect(readerCommand(key)).toBe(command),
  );

  it("ignores unmapped keys", () => {
    expect(gridCommand("x")).toBeUndefined();
    expect(readerCommand("x")).toBeUndefined();
  });

  it("maps help at the app level", () => {
    expect(appCommand("?")).toBe("toggle-help");
    expect(appCommand("Escape")).toBe("close-help");
    expect(gridCommand("?")).toBeUndefined();
  });

  it("maps shift+A to the archive at the app level", () => {
    expect(appCommand("A")).toBe("toggle-archive");
  });

  it("maps unread globally so it remains available without a mounted grid", () => {
    expect(appCommand("a")).toBe("toggle-unread");
    expect(gridCommand("a")).toBeUndefined();
  });
});
