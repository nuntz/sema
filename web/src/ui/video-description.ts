export type DescriptionToken =
  | { kind: "text"; text: string }
  | { kind: "url"; text: string; href: string }
  | { kind: "timestamp"; text: string; href: string; seconds: number };

export type DescriptionBlock =
  | { kind: "paragraph"; tokens: DescriptionToken[] }
  | {
      kind: "chapters";
      rows: Array<{ timestamp: DescriptionToken; tokens: DescriptionToken[] }>;
    };

const URL_OR_TIMESTAMP =
  /(https?:\/\/[^\s<>]+|www\.[^\s<>]+|(?<![\w:])(?:\d{1,2}:)?\d{1,2}:\d{2}(?!\d))/gi;
const LINE_TIMESTAMP = /^\s*((?:\d{1,2}:)?\d{1,2}:\d{2})\s*(.*)$/;
const BOILERPLATE =
  /^\s*[@#]|\b(?:subscribe|affiliate\s+links?|sponsored\s+links?|smash\s+(?:the\s+)?like|like\s+and\s+subscribe)\b/i;
const EMOJI_RUN = /(?:\p{Extended_Pictographic}(?:\uFE0F|\u200D|\s)*){4,}/u;

function timestampSeconds(value: string): number | undefined {
  const parts = value.split(":").map(Number);
  if (parts.some((part) => !Number.isFinite(part)) || parts.length < 2)
    return undefined;
  if (
    (parts[parts.length - 1] ?? 0) >= 60 ||
    (parts[parts.length - 2] ?? 0) >= 60
  )
    return undefined;
  return parts.reduce((total, part) => total * 60 + part, 0);
}

export function timestampURL(videoURL: string, seconds: number): string {
  try {
    const parsed = new URL(videoURL);
    parsed.searchParams.set("t", `${seconds}s`);
    return parsed.toString();
  } catch {
    return videoURL;
  }
}

function displayURL(href: string): string {
  const display = href
    .replace(/^https?:\/\//i, "")
    .replace(/^www\./i, "")
    .replace(/\/$/, "");
  return display.length > 48 ? `${display.slice(0, 47)}…` : display;
}

function tokenize(line: string, videoURL: string): DescriptionToken[] {
  const result: DescriptionToken[] = [];
  let offset = 0;
  for (const match of line.matchAll(URL_OR_TIMESTAMP)) {
    const index = match.index ?? 0;
    if (index > offset)
      result.push({ kind: "text", text: line.slice(offset, index) });
    let value = match[0];
    if (/^(?:https?:\/\/|www\.)/i.test(value)) {
      const trailing = value.match(/[.,;:!?)\]}]+$/)?.[0] ?? "";
      if (trailing) value = value.slice(0, -trailing.length);
      result.push({
        kind: "url",
        text: displayURL(value),
        href: value.startsWith("www.") ? `https://${value}` : value,
      });
      if (trailing) result.push({ kind: "text", text: trailing });
    } else {
      const seconds = timestampSeconds(value);
      if (seconds === undefined) result.push({ kind: "text", text: value });
      else
        result.push({
          kind: "timestamp",
          text: value,
          seconds,
          href: timestampURL(videoURL, seconds),
        });
    }
    offset = index + match[0].length;
  }
  if (offset < line.length)
    result.push({ kind: "text", text: line.slice(offset) });
  return result;
}

export function parseVideoDescription(
  raw: string,
  videoURL: string,
): DescriptionBlock[] {
  const lines = raw
    .replace(/\r\n?/g, "\n")
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line && !BOILERPLATE.test(line) && !EMOJI_RUN.test(line));
  const chapters = lines.filter((line) => LINE_TIMESTAMP.test(line));
  if (chapters.length >= 3) {
    const rows = chapters.flatMap((line) => {
      const match = line.match(LINE_TIMESTAMP);
      if (!match) return [];
      const seconds = timestampSeconds(match[1]);
      if (seconds === undefined) return [];
      return [
        {
          timestamp: {
            kind: "timestamp" as const,
            text: match[1],
            seconds,
            href: timestampURL(videoURL, seconds),
          },
          tokens: tokenize(match[2], videoURL),
        },
      ];
    });
    const prose = lines
      .filter((line) => !LINE_TIMESTAMP.test(line))
      .map((line) => ({
        kind: "paragraph" as const,
        tokens: tokenize(line, videoURL),
      }));
    return [...prose, { kind: "chapters", rows }];
  }
  return lines.map((line) => ({
    kind: "paragraph" as const,
    tokens: tokenize(line, videoURL),
  }));
}
