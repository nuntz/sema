import type { Story } from "../types";

export interface StoryBlockRow {
  stories: Story[];
  template: string;
}

export function blockRows(stories: Story[], width: number): StoryBlockRow[] {
  if (stories.length === 0) return [];
  if (width < 700) return stories.map((story) => row([story], "1fr"));
  if (width < 1000) return chunkRows(stories, 2, "1fr 1fr");
  const rows: StoryBlockRow[] = [];
  const first = stories.slice(0, 2);
  rows.push(row(first, first.length === 1 ? "1fr" : "1.55fr 1fr"));
  for (let index = 2; index < stories.length; index += 3) {
    const group = stories.slice(index, index + 3);
    rows.push(
      row(group, group.length === 1 ? "1fr" : `repeat(${group.length}, 1fr)`),
    );
  }
  return rows;
}

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

function chunkRows(
  stories: Story[],
  size: number,
  template: string,
): StoryBlockRow[] {
  const rows: StoryBlockRow[] = [];
  for (let index = 0; index < stories.length; index += size) {
    const group = stories.slice(index, index + size);
    rows.push(row(group, group.length === 1 ? "1fr" : template));
  }
  return rows;
}

function row(stories: Story[], template: string): StoryBlockRow {
  return { stories, template };
}
