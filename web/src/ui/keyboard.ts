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
  | "unread";
export type AppCommand = "toggle-help" | "close-help";
export type ReaderCommand =
  | "close"
  | "next"
  | "previous"
  | "like"
  | "dislike"
  | "heart"
  | "copy"
  | "original";

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
  G: "end",
  Home: "home",
  g: "go-prefix",
  u: "undo",
  c: "copy",
  v: "original",
  t: "order",
  a: "unread",
};

const appBindings: Record<string, AppCommand> = {
  "?": "toggle-help",
  Escape: "close-help",
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
};

export const gridCommand = (key: string): GridCommand | undefined =>
  gridBindings[key];
export const appCommand = (key: string): AppCommand | undefined =>
  appBindings[key];
export const readerCommand = (key: string): ReaderCommand | undefined =>
  readerBindings[key];
