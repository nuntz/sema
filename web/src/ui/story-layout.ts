import type { Story } from "../types";

export function headlineSlice(story: Story): {
  items: Story["items"];
  remaining: number;
} {
  const headlines = story.items.slice(1);
  return {
    items: headlines.slice(0, 5),
    remaining: Math.max(0, headlines.length - 5),
  };
}

export function sheetHeadlineSlice(story: Story): {
  items: Story["items"];
  remaining: number;
} {
  const headlines = story.items.slice(1);
  const itemLimit = headlines.length > 6 ? 5 : 6;
  return {
    items: headlines.slice(0, itemLimit),
    remaining: Math.max(0, headlines.length - itemLimit),
  };
}
