import { afterEach, describe, expect, it, vi } from "vitest";
import { createVideoPlayer, videoDwellActive } from "./youtube-player";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});
describe("video dwell", () => {
  it("continues through iframe blur while playing, but stops on pause, end or hidden", () => {
    expect(videoDwellActive("playing", true, false)).toBe(true);
    expect(videoDwellActive("buffering", true, false)).toBe(true);
    for (const state of ["paused", "ended", "loading", "poster"] as const)
      expect(videoDwellActive(state, true, false)).toBe(false);
    expect(videoDwellActive("playing", false, true)).toBe(false);
    expect(videoDwellActive("poster", true, true)).toBe(true);
    expect(videoDwellActive("paused", true, true)).toBe(false);
  });
});
it("seeks the latest timestamp after loading, exposes position, and destroys the player", async () => {
  vi.useFakeTimers();
  let options:
    | ConstructorParameters<NonNullable<Window["YT"]>["Player"]>[1]
    | undefined;
  const target = {
    seekTo: vi.fn(),
    playVideo: vi.fn(),
    getCurrentTime: () => 87,
    destroy: vi.fn(),
  };
  const Player = vi.fn((_host, value) => {
    options = value;
    return target;
  });
  vi.stubGlobal("window", {
    YT: { Player },
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    setTimeout,
  });
  vi.stubGlobal("navigator", { onLine: true });
  vi.stubGlobal("location", { origin: "https://sema.test" });
  vi.stubGlobal("document", { createElement: vi.fn() });
  const onFailure = vi.fn();
  const onState = vi.fn();
  const host = { append: vi.fn() } as unknown as HTMLElement;
  const player = createVideoPlayer(host, {
    videoID: "dQw4w9WgXcQ",
    start: 0,
    onFailure,
    onState,
  });
  player.play(42);
  await Promise.resolve();
  expect(options?.host).toBe("https://www.youtube-nocookie.com");
  options?.events.onReady({ target });
  expect(target.seekTo).toHaveBeenLastCalledWith(42, true);
  player.play(60);
  expect(target.seekTo).toHaveBeenLastCalledWith(60, true);
  expect(player.position()).toBe(87);
  options?.events.onStateChange({ data: 1 });
  expect(onState).toHaveBeenCalledWith("playing");
  player.destroy();
  player.destroy();
  expect(target.destroy).toHaveBeenCalledTimes(1);
  options?.events.onError();
  expect(onFailure).not.toHaveBeenCalled();

  const failing = createVideoPlayer(host, {
    videoID: "dQw4w9WgXcQ",
    start: 0,
    onFailure,
    onState,
  });
  await Promise.resolve();
  options?.events.onError();
  options?.events.onError();
  failing.destroy();
  expect(onFailure).toHaveBeenCalledTimes(1);
  expect(target.destroy).toHaveBeenCalledTimes(2);

  Player.mockClear();
  const pending = createVideoPlayer(host, {
    videoID: "dQw4w9WgXcQ",
    start: 0,
    onFailure,
    onState,
  });
  pending.destroy();
  await Promise.resolve();
  expect(Player).not.toHaveBeenCalled();
});
