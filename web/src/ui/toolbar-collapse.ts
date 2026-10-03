export const TOOLBAR_COLLAPSE_TRAVEL = 120;

export interface ToolbarCollapseState {
  collapsed: boolean;
  downwardTravel: number;
  lastScrollTop: number;
}

export function initialToolbarCollapseState(
  scrollTop = 0,
): ToolbarCollapseState {
  return {
    collapsed: false,
    downwardTravel: 0,
    lastScrollTop: scrollTop,
  };
}

export function updateToolbarCollapse(
  state: ToolbarCollapseState,
  scrollTop: number,
  atBottom: boolean,
): ToolbarCollapseState {
  const nextTop = Math.max(0, scrollTop);
  const delta = nextTop - state.lastScrollTop;
  if (atBottom || delta < 0) return initialToolbarCollapseState(nextTop);
  if (delta === 0) return { ...state, lastScrollTop: nextTop };

  const downwardTravel = state.downwardTravel + delta;
  return {
    collapsed: state.collapsed || downwardTravel >= TOOLBAR_COLLAPSE_TRAVEL,
    downwardTravel,
    lastScrollTop: nextTop,
  };
}

export function expandToolbar(
  state: ToolbarCollapseState,
): ToolbarCollapseState {
  return initialToolbarCollapseState(state.lastScrollTop);
}
