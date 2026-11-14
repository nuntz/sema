import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fitSummaryLines } from "./summary-fit";

// The unit tests run without a DOM, so a cell's copy is drawn as plain objects
// that report their own layout: the summary's top, its line height and bottom
// margin, and the top of the bottom line that follows it.
interface Layout {
  summaryTop: number;
  lineHeight: number;
  marginBottom: number;
  bottomLineTop: number;
}

class FakeResizeObserver {
  static latest: FakeResizeObserver;
  observed = new Set<unknown>();
  disconnected = false;
  constructor(private callback: () => void) {
    FakeResizeObserver.latest = this;
  }
  observe(target: unknown) {
    this.observed.add(target);
  }
  unobserve(target: unknown) {
    this.observed.delete(target);
  }
  disconnect() {
    this.disconnected = true;
    this.observed.clear();
  }
  resize() {
    this.callback();
  }
}

function copy(layout: Layout, withBottomLine = true) {
  const properties = new Map<string, string>();
  const attributes = new Set<string>();
  const title = { getBoundingClientRect: () => ({ top: 0 }) };
  const bottomLine = {
    getBoundingClientRect: () => ({ top: layout.bottomLineTop }),
  };
  const summary = {
    style: {
      setProperty: (name: string, value: string) => properties.set(name, value),
    },
    toggleAttribute: (name: string, force: boolean) => {
      if (force) attributes.add(name);
      else attributes.delete(name);
    },
    getBoundingClientRect: () => ({ top: layout.summaryTop }),
    nextElementSibling: withBottomLine ? bottomLine : null,
    parentElement: undefined as unknown,
    computed: () => ({
      lineHeight: `${layout.lineHeight}px`,
      marginBottom: `${layout.marginBottom}px`,
    }),
  };
  const parent = {
    children: withBottomLine ? [title, summary, bottomLine] : [title, summary],
  };
  summary.parentElement = parent;
  return {
    summary: summary as unknown as HTMLElement,
    parent,
    title,
    bottomLine,
    lines: () => properties.get("--summary-lines"),
    noRoom: () => attributes.has("data-no-room"),
  };
}

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", FakeResizeObserver);
  vi.stubGlobal(
    "getComputedStyle",
    (element: { computed: () => Partial<CSSStyleDeclaration> }) =>
      element.computed(),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("fitSummaryLines", () => {
  it("clamps the summary to the whole lines above its bottom line", () => {
    const cell = copy({
      summaryTop: 100,
      lineHeight: 20,
      marginBottom: 8,
      bottomLineTop: 200,
    });
    fitSummaryLines(cell.summary);
    FakeResizeObserver.latest.resize();
    // 92px of room holds four 20px lines.
    expect(cell.lines()).toBe("4");
    expect(cell.noRoom()).toBe(false);
  });

  it("counts a line that is short by under half a pixel", () => {
    const cell = copy({
      summaryTop: 100,
      lineHeight: 20,
      marginBottom: 0,
      bottomLineTop: 159.6,
    });
    fitSummaryLines(cell.summary);
    FakeResizeObserver.latest.resize();
    expect(cell.lines()).toBe("3");
  });

  it("hides a summary that has less than one line of room", () => {
    const cell = copy({
      summaryTop: 100,
      lineHeight: 20,
      marginBottom: 8,
      bottomLineTop: 120,
    });
    fitSummaryLines(cell.summary);
    FakeResizeObserver.latest.resize();
    expect(cell.lines()).toBe("1");
    expect(cell.noRoom()).toBe(true);
  });

  it("refits when what bounds the space resizes, without watching the summary", () => {
    const layout = {
      summaryTop: 100,
      lineHeight: 20,
      marginBottom: 0,
      bottomLineTop: 115,
    };
    const cell = copy(layout);
    fitSummaryLines(cell.summary);
    const observer = FakeResizeObserver.latest;
    observer.resize();
    expect(cell.noRoom()).toBe(true);
    expect([...observer.observed]).toEqual([
      cell.parent,
      cell.title,
      cell.bottomLine,
    ]);

    layout.bottomLineTop = 160;
    observer.resize();
    expect(cell.lines()).toBe("3");
    expect(cell.noRoom()).toBe(false);
  });

  it("leaves a summary with no bottom line unclamped", () => {
    const cell = copy(
      { summaryTop: 100, lineHeight: 20, marginBottom: 0, bottomLineTop: 0 },
      false,
    );
    fitSummaryLines(cell.summary);
    FakeResizeObserver.latest.resize();
    expect(cell.lines()).toBeUndefined();
    expect(cell.noRoom()).toBe(false);
  });

  it("stops fitting once cleaned up", () => {
    const cell = copy({
      summaryTop: 100,
      lineHeight: 20,
      marginBottom: 0,
      bottomLineTop: 200,
    });
    const cleanup = fitSummaryLines(cell.summary);
    cleanup();
    expect(FakeResizeObserver.latest.disconnected).toBe(true);
  });
});
