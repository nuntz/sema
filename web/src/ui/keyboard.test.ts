import { describe, expect, it, vi } from "vitest";
import { effectiveGridOrder } from "../grid-scope";
import {
  allowsAppShortcut,
  appCommand,
  characterShortcut,
  createGoSequence,
  goCommand,
  gridCommand,
  readCharacterShortcuts,
  readerCommand,
  scopeShortcuts,
} from "./keyboard";

describe("keyboard map", () => {
  it.each([
    [" ", "page-down"],
    ["PageDown", "page-down"],
    ["PageUp", "page-up"],
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
    ["i", "image"],
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
    [" ", "page-down"],
    ["PageDown", "page-down"],
    ["PageUp", "page-up"],
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
    expect(gridCommand("g")).toBeUndefined();
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

  it("keeps shift+G scoped to the grid end command", () => {
    expect(appCommand("G")).toBeUndefined();
    expect(gridCommand("G")).toBe("end");
  });

  it("maps unread globally so it remains available without a mounted grid", () => {
    expect(appCommand("a")).toBe("toggle-unread");
    expect(gridCommand("a")).toBeUndefined();
  });

  it("forces chronological order for a feed scope and restores the preference when cleared", () => {
    const preference = "interest" as const;
    expect(
      effectiveGridOrder(preference, { kind: "feed", value: "daily" }),
    ).toBe("chrono");
    expect(effectiveGridOrder(preference, null)).toBe("interest");
  });
});

it("keeps from the reader with Shift+K without changing previous-item k", () => {
  expect(readerCommand("K")).toBe("heart");
  expect(readerCommand("k")).toBe("previous");
});

it("keeps date navigation separate from the unread toggle command", () => {
  expect(["u", "t", "y", "a"].map(goCommand)).toEqual([
    "unread",
    "today",
    "yesterday",
    "all",
  ]);
  expect(scopeShortcuts).toEqual({
    unread: "g → u",
    today: "g → t",
    yesterday: "g → y",
    all: "g → a",
  });
});

// Expected permissions are deliberately independent of the production table.
describe("app shortcut owners", () => {
  const all = [
    "grid",
    "reader",
    "lightbox",
    "action-sheet",
    "related",
    "feeds",
    "keyboard-help",
    "confirm-remove",
    "search",
    "transient",
    "filter-sheet",
    "feeds-dialog",
  ] as const;
  const global = [
    "grid",
    "reader",
    "action-sheet",
    "related",
    "feeds",
    "keyboard-help",
    "confirm-remove",
    "search",
    "transient",
    "feeds-dialog",
  ];
  it.each([
    ["toggle-help", global],
    ["close-help", global],
    [
      "search",
      global.filter((owner) => owner !== "reader" && owner !== "related"),
    ],
    ["go-prefix", ["grid", "feeds"]],
    ["toggle-unread", ["grid", "action-sheet", "transient"]],
    ["toggle-archive", global],
    ["undo", ["grid", "action-sheet", "transient"]],
  ] as const)(
    "preserves %s permissions for every owner",
    (command, allowed) => {
      for (const owner of all) {
        expect(
          allowsAppShortcut(command, {
            owner,
            overlays: owner === "grid" || owner === "transient" ? [] : [owner],
          }),
          owner,
        ).toBe((allowed as readonly string[]).includes(owner));
      }
    },
  );

  it("keeps underlying Reader and search restrictions when help or a transient is on top", () => {
    expect(
      allowsAppShortcut("search", {
        owner: "keyboard-help",
        overlays: ["reader", "keyboard-help"],
      }),
    ).toBe(false);
    expect(
      allowsAppShortcut("toggle-archive", {
        owner: "keyboard-help",
        overlays: ["reader", "keyboard-help"],
      }),
    ).toBe(true);
    expect(
      allowsAppShortcut("undo", { owner: "transient", overlays: ["search"] }),
    ).toBe(false);
    expect(
      allowsAppShortcut("toggle-unread", {
        owner: "action-sheet",
        overlays: ["reader", "action-sheet"],
      }),
    ).toBe(false);
    for (const command of [
      "toggle-help",
      "search",
      "go-prefix",
      "toggle-unread",
      "toggle-archive",
      "undo",
    ] as const) {
      expect(
        allowsAppShortcut(command, {
          owner: "transient",
          overlays: ["lightbox"],
        }),
      ).toBe(false);
      expect(
        allowsAppShortcut(command, {
          owner: "transient",
          overlays: ["filter-sheet"],
        }),
      ).toBe(false);
    }
  });
});

describe("shared g sequence", () => {
  it("routes gg to Grid home and consumes a view sequence once", () => {
    const sequence = createGoSequence();
    expect(sequence.key("g", 1)).toBe("prefix");
    expect(sequence.key("g", 2)).toBe("home");
    expect(sequence.key("u", 3)).toBeUndefined();
    expect(sequence.key("g", 4)).toBe("prefix");
    expect(sequence.key("u", 5)).toBe("unread");
    expect(sequence.key("u", 6)).toBeUndefined();
  });
  it("expires after 600ms and cancels on focus, modifiers, editing, or owner changes", () => {
    const sequence = createGoSequence();
    sequence.key("g", 1);
    expect(sequence.key("u", 601)).toBeUndefined();
    sequence.key("g", 700);
    sequence.clear();
    expect(sequence.key("g", 701)).toBe("prefix");
    expect(sequence.key("x", 702)).toBeUndefined();
    expect(sequence.key("u", 703)).toBeUndefined();
  });
});

it("disables the i character shortcut for both opening and closing, but keeps Escape", () => {
  vi.stubGlobal("localStorage", { getItem: () => "off" });
  try {
    const event = { key: "i", ctrlKey: false, metaKey: false, altKey: false };
    expect(!readCharacterShortcuts() && characterShortcut(event)).toBe(true);
    expect(
      !readCharacterShortcuts() &&
        characterShortcut({ ...event, key: "Escape" }),
    ).toBe(false);
  } finally {
    vi.unstubAllGlobals();
  }
});
