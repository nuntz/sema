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
function setup(start = 0, tabbable?: boolean) {
  vi.useFakeTimers();
  const listeners = new Map<string, (event: unknown) => void>();
  const loads = new Map<string, () => void>();
  const frame = {
    src: "",
    dataset: {} as Record<string, string>,
    contentWindow: { postMessage: vi.fn() },
    remove: vi.fn(),
    addEventListener: vi.fn((name, callback) => loads.set(name, callback)),
    removeEventListener: vi.fn((name) => loads.delete(name)),
  };
  vi.stubGlobal("window", {
    addEventListener: vi.fn((name, callback) => listeners.set(name, callback)),
    removeEventListener: vi.fn((name) => listeners.delete(name)),
    setTimeout,
  });
  vi.stubGlobal("navigator", { onLine: true });
  vi.stubGlobal("location", { origin: "https://sema.test" });
  vi.stubGlobal("document", { createElement: vi.fn(() => frame) });
  const onFailure = vi.fn();
  const onState = vi.fn();
  const host = { append: vi.fn() } as unknown as HTMLElement;
  const player = createVideoPlayer(host, {
    videoID: "dQw4w9WgXcQ",
    start,
    tabbable,
    onFailure,
    onState,
  });
  const message = (
    data: unknown,
    origin = "https://www.youtube-nocookie.com",
    source: unknown = frame.contentWindow,
  ) =>
    listeners.get("message")?.({ data: JSON.stringify(data), origin, source });
  const sent = () =>
    frame.contentWindow.postMessage.mock.calls.map(([data]) =>
      JSON.parse(data),
    );
  return { frame, player, message, sent, listeners, loads, onFailure, onState };
}

it("uses only the privacy embed and queues the latest seek until ready", () => {
  const { frame, player, message, sent, loads } = setup(12);
  const url = new URL(frame.src);
  expect(url.origin).toBe("https://www.youtube-nocookie.com");
  expect(url.pathname).toBe("/embed/dQw4w9WgXcQ");
  expect(Object.fromEntries(url.searchParams)).toEqual({
    enablejsapi: "1",
    autoplay: "1",
    playsinline: "1",
    start: "12",
    origin: "https://sema.test",
  });
  expect(frame).toMatchObject({
    allow: "autoplay; fullscreen; picture-in-picture; encrypted-media",
    referrerPolicy: "strict-origin-when-cross-origin",
    allowFullscreen: true,
  });
  player.play(42);
  player.play(60);
  expect(sent()).toEqual([]);
  loads.get("load")?.();
  expect(sent()[0]).toMatchObject({
    event: "listening",
    channel: "widget",
    id: expect.any(String),
  });
  message({ event: "onReady" });
  expect(sent().slice(1)).toMatchObject([
    { event: "command", func: "seekTo", args: [60, true] },
    { event: "command", func: "playVideo", args: [] },
  ]);
  message({ event: "infoDelivery", info: { currentTime: 87, playerState: 1 } });
  expect(player.position()).toBe(87);
  player.play(90);
  expect(sent().at(-2)).toMatchObject({ func: "seekTo", args: [90, true] });
  for (const [, target] of frame.contentWindow.postMessage.mock.calls)
    expect(target).toBe(url.origin);
  player.destroy();
});

it("keeps the iframe out of the Tab order only when asked", () => {
  expect(setup().frame).not.toHaveProperty("tabIndex");
  vi.unstubAllGlobals();
  const { frame } = setup(0, false);
  expect(frame).toHaveProperty("tabIndex", -1);
});

it("rejects spoofed and malformed messages and accepts infoDelivery as ready", () => {
  const { frame, player, message, sent, loads, listeners, onState, onFailure } =
    setup();
  const info = {
    event: "infoDelivery",
    info: { currentTime: 87, playerState: 1 },
  };
  message(info);
  expect(player.position()).toBe(0);
  loads.get("load")?.();
  message(info, "https://www.youtube.com");
  message(info, "https://www.youtube-nocookie.com", {});
  listeners.get("message")?.({
    data: "not json",
    origin: "https://www.youtube-nocookie.com",
    source: frame.contentWindow,
  });
  expect(onState).not.toHaveBeenCalled();
  expect(player.position()).toBe(0);
  message(info);
  expect(player.position()).toBe(87);
  expect(sent().at(-1)).toMatchObject({ func: "playVideo" });
  for (const [playerState, state] of [
    [0, "ended"],
    [1, "playing"],
    [2, "paused"],
    [3, "buffering"],
    [5, "paused"],
  ]) {
    message({ event: "infoDelivery", info: { playerState } });
    expect(onState).toHaveBeenLastCalledWith(state);
  }
  message({ event: "onError" });
  message({ event: "onError" });
  expect(onFailure).toHaveBeenCalledTimes(1);
  player.destroy();
});

it("cleans up the iframe, load/message/offline listeners and timeout on destroy", () => {
  const { player, frame, loads, listeners, onFailure } = setup();
  player.destroy();
  player.destroy();
  expect(frame.remove).toHaveBeenCalledTimes(1);
  expect(loads.size).toBe(0);
  expect(listeners.size).toBe(0);
  vi.advanceTimersByTime(15_000);
  player.play(12);
  expect(onFailure).not.toHaveBeenCalled();
  expect(frame.contentWindow.postMessage).not.toHaveBeenCalled();
});

it.each(["timeout", "offline"])(
  "fails once on %s and removes the iframe",
  (reason) => {
    const { player, frame, listeners, onFailure } = setup();
    if (reason === "offline") listeners.get("offline")?.({});
    vi.advanceTimersByTime(15_000);
    expect(onFailure).toHaveBeenCalledTimes(1);
    expect(frame.remove).toHaveBeenCalledTimes(1);
    expect(listeners.size).toBe(0);
    player.destroy();
  },
);
