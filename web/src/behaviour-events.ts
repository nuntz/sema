export type BehaviourEvent = {
  dwell_ms?: number;
  clicked_through?: true;
  shared?: true;
};

export function linkBehaviourEvent(): BehaviourEvent {
  return { clicked_through: true, shared: true };
}

export function mergeBehaviourEvent(
  current: BehaviourEvent,
  next: BehaviourEvent,
): BehaviourEvent {
  const dwell = Math.max(current.dwell_ms ?? 0, next.dwell_ms ?? 0);
  return {
    dwell_ms: dwell || undefined,
    clicked_through:
      current.clicked_through || next.clicked_through ? true : undefined,
    shared: current.shared || next.shared ? true : undefined,
  };
}
