import type { Item } from "./types";

const entities: Record<string, string> = {
  amp: "&",
  quot: '"',
  apos: "'",
  lt: "<",
  gt: ">",
  nbsp: " ",
  lsquo: "‘",
  rsquo: "’",
  ldquo: "“",
  rdquo: "”",
  ndash: "–",
  mdash: "—",
  hellip: "…",
};

// Decode text only; feed content is still rendered through Solid's text nodes.
export function headlineText(value: string): string {
  return value.replace(
    /&(#x[\da-f]+|#\d+|[a-z]+);/gi,
    (entity, name: string) => {
      if (!name.startsWith("#"))
        return Object.hasOwn(entities, name) ? entities[name] : entity;
      const hex = name.slice(0, 2).toLowerCase() === "#x";
      const point = Number.parseInt(name.slice(hex ? 2 : 1), hex ? 16 : 10);
      return point > 0 &&
        point <= 0x10ffff &&
        !(point >= 0xd800 && point <= 0xdfff)
        ? String.fromCodePoint(point)
        : entity;
    },
  );
}

export function gridSourceName(item: Pick<Item, "feed_title">): string {
  const title = headlineText(item.feed_title || "Feed").trim();
  const known: Record<string, string> = {
    "hacker news: front page": "Hacker News",
    "hn hacker news: front page": "Hacker News",
    "www.theregister.com - articles": "The Register",
    "eurogamer.net latest articles feed": "Eurogamer",
    "ign all": "IGN",
    "polygon.com": "Polygon",
  };
  return Object.hasOwn(known, title.toLowerCase())
    ? known[title.toLowerCase()]
    : title;
}

const normalHeadline = (value: string) =>
  headlineText(value)
    .toLowerCase()
    .replace(/[’']/g, "")
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim();

export function repeatsLeadHeadline(lead: string, related: string): boolean {
  const left = normalHeadline(lead);
  const right = normalHeadline(related);
  if (!left || !right) return false;
  if (left === right) return true;
  // Preserve differences in quantities and negation even in otherwise identical titles.
  const facts = (value: string) =>
    value.match(/\b(?:\d+|no|not|never|without|\w+nt)\b/g)?.join(" ") ?? "";
  if (facts(left) !== facts(right)) return false;
  const length = Math.max(left.length, right.length);
  if (length > 500 || Math.abs(left.length - right.length) > length * 0.1)
    return false;
  let previous = Array.from({ length: right.length + 1 }, (_, index) => index);
  for (let i = 1; i <= left.length; i++) {
    const row = [i];
    for (let j = 1; j <= right.length; j++) {
      row[j] = Math.min(
        row[j - 1] + 1,
        previous[j] + 1,
        previous[j - 1] + (left[i - 1] === right[j - 1] ? 0 : 1),
      );
    }
    previous = row;
  }
  return previous[right.length] / length <= 0.1;
}

export function relatedCoverageHeight(lead: Item, related: Item): number {
  return repeatsLeadHeadline(lead.title, related.title) ? 44 : 72;
}
