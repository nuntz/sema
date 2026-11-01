// Compact age for dense chrome: "now", "3m", "2h", "5d".
export function relativeAge(value: string, now: number): string {
  const seconds = Math.max(0, (now - new Date(value).getTime()) / 1000);
  if (seconds < 60) return "now";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}

// Turns a compact age into a phrase: "just now", "3m ago". Use this instead of appending "ago".
export function agoPhrase(age: string): string {
  return age === "now" ? "just now" : `${age} ago`;
}

export function relativeAgo(value: string, now: number): string {
  return agoPhrase(relativeAge(value, now));
}
