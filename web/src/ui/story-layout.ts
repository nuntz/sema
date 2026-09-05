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
