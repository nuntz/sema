import { deadlineGroup, deadlineTime } from "./expiring-view";
import { expirySentence, HOUR, hoursLeft, LIFETIME } from "./expiry";

export function readerDay(published: string, now: number): number {
  return Math.max(
    1,
    Math.min(7, Math.floor((now - Date.parse(published)) / (24 * HOUR)) + 1),
  );
}

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
