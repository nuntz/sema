import { describe, expect, it } from "vitest";
import { createInputModality } from "./input-modality";

const pointerDown = (pointerType: string) =>
  Object.assign(new Event("pointerdown"), { pointerType });

describe("input modality", () => {
  it("follows the last key, mouse or touch press", () => {
    const target = new EventTarget();
    const modality = createInputModality(target);
    expect(modality.last()).toBeUndefined();

    target.dispatchEvent(new Event("keydown"));
    expect(modality.last()).toBe("keyboard");

    target.dispatchEvent(pointerDown("mouse"));
    expect(modality.last()).toBe("mouse");

    target.dispatchEvent(pointerDown("touch"));
    expect(modality.last()).toBe("touch");

    target.dispatchEvent(new Event("keydown"));
    target.dispatchEvent(new Event("touchstart"));
    expect(modality.last()).toBe("touch");
  });

  it("counts a pen as touch, since it swipes", () => {
    const target = new EventTarget();
    const modality = createInputModality(target);

    target.dispatchEvent(pointerDown("pen"));
    expect(modality.last()).toBe("touch");
  });

  it("slides core navigation only for touch, and keeps an occasional surface still only for a key", () => {
    const target = new EventTarget();
    const modality = createInputModality(target);
    const policy = () => [
      modality.slidesCoreNavigation(),
      modality.movesOccasionalSurface(),
    ];
    expect(policy()).toEqual([false, true]);

    target.dispatchEvent(new Event("keydown"));
    expect(policy()).toEqual([false, false]);

    target.dispatchEvent(pointerDown("mouse"));
    expect(policy()).toEqual([false, true]);

    target.dispatchEvent(pointerDown("touch"));
    expect(policy()).toEqual([true, true]);
  });

  it("sees a key that a later capture listener stops", () => {
    const target = new EventTarget();
    const modality = createInputModality(target);
    target.addEventListener(
      "keydown",
      (event) => event.stopImmediatePropagation(),
      true,
    );

    target.dispatchEvent(new Event("keydown"));
    expect(modality.last()).toBe("keyboard");
  });

  it("stops listening once disposed", () => {
    const target = new EventTarget();
    const modality = createInputModality(target);
    modality.dispose();

    for (const event of [
      new Event("keydown"),
      pointerDown("touch"),
      new Event("touchstart"),
    ]) {
      target.dispatchEvent(event);
      expect(modality.last(), event.type).toBeUndefined();
    }
  });
});
