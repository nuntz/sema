import { expirySentence, hoursLeft, LIFETIME } from "./expiry";

export function readerDeadlineLine(published: string, now: number): string {
  const hours = hoursLeft(published, now);
  if (hours <= 0) return "Goes now unless you keep it.";
  const group = deadlineGroup(published, now);
  const day =
    group === "later"
      ? new Date(Date.parse(published) + LIFETIME).toLocaleDateString(
          undefined,
          { weekday: "long" },
        )
      : group;
  const time = deadlineTime(published);
  if (hours < 6)
    return `Goes at ${time} ${day} — ${expirySentence(published, now).replace(/ left$/, "")}.`;
  return `Goes ${day} at ${time} unless you keep it.`;
}

export function nextMidnight(now: number): Date {
  const date = new Date(now);
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + 1);
}
export function deadlineGroup(
  published: string,
  now: number,
): "tonight" | "tomorrow" | "later" {
  const deadline = Date.parse(published) + LIFETIME;
  const midnight = nextMidnight(now);
  if (deadline < midnight.getTime()) return "tonight";
  midnight.setDate(midnight.getDate() + 1);
  return deadline < midnight.getTime() ? "tomorrow" : "later";
}
export function deadlineTime(published: string): string {
  return new Date(Date.parse(published) + LIFETIME).toLocaleTimeString(
    undefined,
    { hour: "numeric", minute: "2-digit" },
  );
}
