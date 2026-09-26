import type { ItemWindow } from "../item-view";
import type { KeyOwner, KeyOwnership, OverlayKind } from "./overlay-history";

export type GridCommand =
  | "page-down"
  | "page-up"
  | "down"
  | "up"
  | "left"
  | "right"
  | "open"
  | "like"
  | "dislike"
  | "heart"
  | "read"
  | "mark-below"
  | "end"
  | "home"
  | "undo"
  | "copy"
  | "original"
  | "image"
  | "order"
  | "related";
export type AppCommand =
  | "toggle-help"
  | "close-help"
  | "toggle-archive"
  | "toggle-unread";
export type ReaderCommand =
  | "play"
  | "close"
  | "next"
  | "previous"
  | "page-down"
  | "page-up"
  | "like"
  | "dislike"
  | "heart"
  | "copy"
  | "original"
  | "related";

const gridBindings: Record<string, GridCommand> = {
  " ": "page-down",
  PageDown: "page-down",
  PageUp: "page-up",
  j: "down",
  ArrowDown: "down",
  k: "up",
  ArrowUp: "up",
  h: "left",
  ArrowLeft: "left",
  l: "right",
  ArrowRight: "right",
  Enter: "open",
  o: "open",
  "+": "like",
  ".": "like",
  "-": "dislike",
  ",": "dislike",
  f: "heart",
  m: "read",
  M: "mark-below",
  End: "end",
  G: "end",
  Home: "home",
  u: "undo",
  c: "copy",
  i: "image",
  v: "original",
  t: "order",
  r: "related",
};

const appBindings: Record<string, AppCommand> = {
  "?": "toggle-help",
  Escape: "close-help",
  A: "toggle-archive",
  a: "toggle-unread",
};

const readerBindings: Record<string, ReaderCommand> = {
  i: "play",
  Escape: "close",
  n: "next",
  j: "next",
  p: "previous",
  k: "previous",
  " ": "page-down",
  PageDown: "page-down",
  PageUp: "page-up",
  "+": "like",
  ".": "like",
  "-": "dislike",
  ",": "dislike",
  f: "heart",
  K: "heart",
  c: "copy",
  v: "original",
  r: "related",
};

export const gridCommand = (key: string): GridCommand | undefined =>
  gridBindings[key];
export const appCommand = (key: string): AppCommand | undefined =>
  appBindings[key];
export const readerCommand = (key: string): ReaderCommand | undefined =>
  readerBindings[key];

export const scopeShortcuts: Record<"unread" | ItemWindow, string> = {
  unread: "g → u",
  today: "g → t",
  yesterday: "g → y",
  all: "g → a",
};
export type GoCommand = "unread" | ItemWindow | "archive" | "settings";
export const goCommand = (key: string): GoCommand | undefined =>
  (
    ({
      u: "unread",
      t: "today",
      y: "yesterday",
      a: "all",
      r: "archive",
      s: "settings",
    }) as Record<string, GoCommand>
  )[key];

export function isEditingTarget(target: EventTarget | null): boolean {
  return (
    target instanceof HTMLElement &&
    (target.isContentEditable || target.matches("input, textarea, select"))
  );
}

export const characterShortcut = (
  event: Pick<KeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey">,
): boolean =>
  event.key.length === 1 &&
  event.key !== " " &&
  !event.ctrlKey &&
  !event.metaKey &&
  !event.altKey;

export function readCharacterShortcuts(): boolean {
  try {
    return localStorage.getItem("sema:character-shortcuts") !== "off";
  } catch {
    return true;
  }
}

// Keep the historical permissions explicit, including overlays underneath help.
// Transient menus suppress sequences, but still permit global view shortcuts.
const globalOwners: readonly KeyOwner[] = [
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
const gridBlockers: readonly OverlayKind[] = [
  "reader",
  "keyboard-help",
  "confirm-remove",
  "search",
  "related",
  "feeds",
];
export const appShortcutPermissions = {
  "toggle-help": { owners: globalOwners, blocked: [] },
  "close-help": { owners: globalOwners, blocked: [] },
  search: { owners: globalOwners, blocked: ["reader", "related"] },
  "go-prefix": {
    owners: ["grid", "feeds"],
    blocked: [
      "reader",
      "keyboard-help",
      "confirm-remove",
      "search",
      "related",
      "action-sheet",
      "feeds-dialog",
    ],
  },
  "toggle-unread": {
    owners: ["grid", "action-sheet", "transient"],
    blocked: gridBlockers,
  },
  "toggle-archive": { owners: globalOwners, blocked: [] },
  undo: {
    owners: ["grid", "action-sheet", "transient"],
    blocked: gridBlockers,
  },
} satisfies Record<
  string,
  { owners: readonly KeyOwner[]; blocked: readonly OverlayKind[] }
>;

export function allowsAppShortcut(
  command: keyof typeof appShortcutPermissions,
  ownership: KeyOwnership,
): boolean {
  const rule: { owners: readonly KeyOwner[]; blocked: readonly OverlayKind[] } =
    appShortcutPermissions[command];
  return (
    rule.owners.includes(ownership.owner) &&
    !ownership.overlays.some(
      (kind) =>
        kind === "lightbox" ||
        kind === "filter-sheet" ||
        rule.blocked.includes(kind),
    )
  );
}

// The App owns the single sequence; Grid only supplies the home action.
export function createGoSequence() {
  let deadline = 0;
  return {
    clear() {
      deadline = 0;
    },
    key(key: string, now: number): GoCommand | "home" | "prefix" | undefined {
      const pending = deadline > now;
      deadline = 0;
      if (pending) return key === "g" ? "home" : goCommand(key);
      if (key === "g") {
        deadline = now + 600;
        return "prefix";
      }
    },
  };
}
