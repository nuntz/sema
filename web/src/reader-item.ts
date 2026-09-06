import type { Item } from "./types";

function normalizeComparable(value: string): string {
  return value.toLocaleLowerCase().replace(/[^\p{L}\p{N}]/gu, "");
}

export function stripSummaryEcho(summary: string, title: string): string {
  let cleaned = summary.trim().replace(/\s+/gu, " ");
  cleaned = cleaned.replace(/^"+|"+$/gu, "").trim();
  cleaned = cleaned.replace(/^title\s*:\s*/iu, "");

  const titleRunes = Array.from(title.trim());
  const summaryRunes = Array.from(cleaned);
  if (titleRunes.length === 0 || summaryRunes.length < titleRunes.length)
    return cleaned.trim();

  const prefix = summaryRunes.slice(0, titleRunes.length).join("");
  if (normalizeComparable(prefix) !== normalizeComparable(title))
    return cleaned.trim();
  if (/^[\p{L}\p{N}]$/u.test(summaryRunes[titleRunes.length] ?? ""))
    return cleaned.trim();

  return summaryRunes
    .slice(titleRunes.length)
    .join("")
    .replace(/^[\s\p{P}]*/u, "");
}

export function resolveReaderItem(
  itemID: string,
  collections: readonly (readonly Item[])[],
  retained?: Item,
): Item | undefined {
  for (const items of collections) {
    const item = items.find((candidate) => candidate.item_id === itemID);
    if (item) return item;
  }
  return retained?.item_id === itemID ? retained : undefined;
}
