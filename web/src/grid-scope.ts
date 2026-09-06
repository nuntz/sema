import type { GridScope, Order } from "./types";

export function effectiveGridOrder(preference: Order, scope: GridScope): Order {
  return scope?.kind === "feed" ? "chrono" : preference;
}

export function sameGridScope(first: GridScope, second: GridScope): boolean {
  return first?.kind === second?.kind && first?.value === second?.value;
}
