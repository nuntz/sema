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
  | "undo"
  | "original"
  | "order"
  | "unread"
  | "help";
export type ReaderCommand =
  | "close"
  | "next"
  | "previous"
  | "like"
  | "dislike"
  | "heart"
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
  u: "undo",
  v: "original",
  t: "order",
  a: "unread",
  "?": "help",
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
  v: "original",
};

export const gridCommand = (key: string): GridCommand | undefined =>
  gridBindings[key];
export const readerCommand = (key: string): ReaderCommand | undefined =>
  readerBindings[key];
