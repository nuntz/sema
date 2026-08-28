import { describe, expect, it, vi } from "vitest";
import { listenForWindowReturn } from "./window-activity";

describe("window activity", () => {
  it("checks for new items when a visible browser window regains focus", () => {
    const documentTarget = new EventTarget();
    const windowTarget = new EventTarget();
    let visibilityState: DocumentVisibilityState = "visible";
    Object.defineProperty(documentTarget, "visibilityState", {
      get: () => visibilityState,
    });
    const onHidden = vi.fn();
    const onReturn = vi.fn();
    const stop = listenForWindowReturn(
      onHidden,
      onReturn,
      documentTarget as Document,
      windowTarget as Window,
    );

    windowTarget.dispatchEvent(new Event("focus"));
    expect(onReturn).toHaveBeenCalledTimes(1);

    visibilityState = "hidden";
    windowTarget.dispatchEvent(new Event("focus"));
    expect(onReturn).toHaveBeenCalledTimes(1);

    documentTarget.dispatchEvent(new Event("visibilitychange"));
    expect(onHidden).toHaveBeenCalledTimes(1);

    visibilityState = "visible";
    documentTarget.dispatchEvent(new Event("visibilitychange"));
    expect(onReturn).toHaveBeenCalledTimes(2);

    const restored = new Event("pageshow");
    Object.defineProperty(restored, "persisted", { value: true });
    windowTarget.dispatchEvent(restored);
    windowTarget.dispatchEvent(new Event("online"));
    expect(onReturn).toHaveBeenCalledTimes(4);

    windowTarget.dispatchEvent(new Event("pageshow"));
    expect(onReturn).toHaveBeenCalledTimes(4);

    stop();
    windowTarget.dispatchEvent(new Event("focus"));
    windowTarget.dispatchEvent(restored);
    windowTarget.dispatchEvent(new Event("online"));
    documentTarget.dispatchEvent(new Event("visibilitychange"));
    expect(onReturn).toHaveBeenCalledTimes(4);
    expect(onHidden).toHaveBeenCalledTimes(1);
  });
});
