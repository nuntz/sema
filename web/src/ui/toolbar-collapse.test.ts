import { describe, expect, it } from "vitest";
import {
  expandToolbar,
  initialToolbarCollapseState,
  updateToolbarCollapse,
} from "./toolbar-collapse";

describe("reader toolbar collapse", () => {
  it("collapses after 120 pixels of continuous downward travel", () => {
    let state = initialToolbarCollapseState();
    state = updateToolbarCollapse(state, 70, false);
    expect(state.collapsed).toBe(false);

    state = updateToolbarCollapse(state, 120, false);
    expect(state.collapsed).toBe(true);
  });

  it("expands immediately when scrolling upward", () => {
    const collapsed = updateToolbarCollapse(
      initialToolbarCollapseState(),
      140,
      false,
    );

    expect(updateToolbarCollapse(collapsed, 139, false)).toEqual({
      collapsed: false,
      downwardTravel: 0,
      lastScrollTop: 139,
    });
  });

  it("expands at the bottom regardless of scroll direction", () => {
    const collapsed = updateToolbarCollapse(
      initialToolbarCollapseState(),
      140,
      false,
    );

    expect(updateToolbarCollapse(collapsed, 150, true).collapsed).toBe(false);
  });

  it("expands for a tap, focus, or item reset", () => {
    const collapsed = updateToolbarCollapse(
      initialToolbarCollapseState(),
      140,
      false,
    );

    expect(expandToolbar(collapsed)).toEqual(initialToolbarCollapseState(140));
    expect(initialToolbarCollapseState()).toEqual({
      collapsed: false,
      downwardTravel: 0,
      lastScrollTop: 0,
    });
  });
});
