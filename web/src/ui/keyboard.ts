import type { ItemView } from "../item-view";

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
  | "go-prefix"
  | "undo"
  | "copy"
  | "original"
  | "order"
  | "related";
export type AppCommand =
  | "toggle-help"
  | "close-help"
  | "toggle-archive"
  | "toggle-unread";
export type ReaderCommand =
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
  g: "go-prefix",
  u: "undo",
  c: "copy",
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

export const scopeShortcuts: Record<ItemView, string> = {
  unread: "g → u",
  today: "g → t",
  yesterday: "g → y",
  all: "g → a",
};
export type GoCommand = ItemView | "archive" | "settings";
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

export const characterShortcut = (event: KeyboardEvent): boolean =>
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
