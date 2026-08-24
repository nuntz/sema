export type GridCommand =
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
  | "toggle-unread"
  | "open-settings";
export type ReaderCommand =
  | "close"
  | "next"
  | "previous"
  | "like"
  | "dislike"
  | "heart"
  | "copy"
  | "original"
  | "related";

const gridBindings: Record<string, GridCommand> = {
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
  G: "open-settings",
  a: "toggle-unread",
};

const readerBindings: Record<string, ReaderCommand> = {
  Escape: "close",
  n: "next",
  j: "next",
  p: "previous",
  k: "previous",
  "+": "like",
  ".": "like",
  "-": "dislike",
  ",": "dislike",
  f: "heart",
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
