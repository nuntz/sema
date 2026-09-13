import { describe, expect, it } from "vitest";
import {
  createOverlayHistory,
  type OverlayHistoryLike,
  type PopStateSource,
} from "./overlay-history";

class FakeBrowser implements OverlayHistoryLike, PopStateSource {
  entries: unknown[] = [{ sema: "reader" }];
  index = 0;
  pushes = 0;
  backs = 0;
  private listeners = new Set<(event: PopStateEvent) => void>();

  get state(): unknown {
    return this.entries[this.index];
  }

  pushState(data: unknown): void {
    this.entries.splice(this.index + 1, Infinity, data);
    this.index++;
    this.pushes++;
  }

  back(): void {
    this.backs++;
  }

  addEventListener(
    _type: "popstate",
    listener: (event: PopStateEvent) => void,
  ): void {
    this.listeners.add(listener);
  }

  removeEventListener(
    _type: "popstate",
    listener: (event: PopStateEvent) => void,
  ): void {
    this.listeners.delete(listener);
  }

  goTo(index: number): void {
    this.index = index;
    const event = { state: this.state } as PopStateEvent;
    for (const listener of this.listeners) listener(event);
  }
}

describe("overlay history", () => {
  it("replaces the grid lightbox with the reader before the next Back", () => {
    const browser = new FakeBrowser();
    const overlays = createOverlayHistory(browser, browser);
    const closed: string[] = [];
    overlays.pushOverlay("lightbox", () => closed.push("lightbox"));
    overlays.closeOverlay("lightbox");
    overlays.pushOverlay("reader", () => closed.push("reader"));
    expect(browser.pushes).toBe(1);
    browser.goTo(0);
    expect(browser.entries).toHaveLength(2);
    expect(browser.state).toMatchObject({ sema: "reader" });
    browser.goTo(0);
    expect(closed).toEqual(["reader"]);
  });
  it("pops overlays in stack order", () => {
    const browser = new FakeBrowser();
    const overlays = createOverlayHistory(browser, browser);
    const closed: string[] = [];
    overlays.pushOverlay("reader", () => closed.push("reader"));
    overlays.pushOverlay("related", () => closed.push("related"));

    browser.goTo(1);
    expect(closed).toEqual(["related"]);
    browser.goTo(0);
    expect(closed).toEqual(["related", "reader"]);
  });

  it("closes every overlay above a multi-entry pop target", () => {
    const browser = new FakeBrowser();
    const overlays = createOverlayHistory(browser, browser);
    const closed: string[] = [];
    overlays.pushOverlay("reader", () => closed.push("reader"));
    overlays.pushOverlay("related", () => closed.push("related"));
    overlays.pushOverlay("keyboard-help", () => closed.push("keyboard"));

    browser.goTo(0);
    expect(closed).toEqual(["keyboard", "related", "reader"]);
  });

  it("backs exactly once for a UI close without firing its callback", () => {
    const browser = new FakeBrowser();
    const overlays = createOverlayHistory(browser, browser);
    let closes = 0;
    overlays.pushOverlay("reader", () => closes++);

    overlays.closeOverlay("reader");
    expect(browser.backs).toBe(1);
    browser.goTo(0);
    expect(closes).toBe(0);
  });

  it("does not push when content replaces an overlay of the same kind", () => {
    const browser = new FakeBrowser();
    const overlays = createOverlayHistory(browser, browser);
    const closed: string[] = [];
    overlays.pushOverlay("reader", () => closed.push("first"));
    overlays.pushOverlay("reader", () => closed.push("replacement"));

    expect(browser.pushes).toBe(1);
    browser.goTo(0);
    expect(closed).toEqual(["replacement"]);
  });

  it("defers a replacement push until a programmatic pop settles", () => {
    const browser = new FakeBrowser();
    const overlays = createOverlayHistory(browser, browser);
    overlays.pushOverlay("action-sheet", () => undefined);
    overlays.closeOverlay("action-sheet");
    overlays.pushOverlay("related", () => undefined);

    expect(browser.pushes).toBe(1);
    browser.goTo(0);
    expect(browser.pushes).toBe(2);
  });
});
