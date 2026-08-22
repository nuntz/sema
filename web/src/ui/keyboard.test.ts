import { describe, expect, it } from "vitest";
import { gridCommand, readerCommand } from "./keyboard";

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
    ["v", "original"],
    ["t", "order"],
    ["a", "unread"],
    ["?", "help"],
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
    ["v", "original"],
  ])("maps reader key %s", (key, command) =>
    expect(readerCommand(key)).toBe(command),
  );

  it("ignores unmapped keys", () => {
    expect(gridCommand("x")).toBeUndefined();
    expect(readerCommand("x")).toBeUndefined();
  });
});
