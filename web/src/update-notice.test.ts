import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createUpdateNotice, updateMeta } from "./update-notice";

function setup(saved = "[]") {
  let visible = true;
  const storage = { getItem: vi.fn(() => saved), setItem: vi.fn() };
  const fetch = vi.fn(async () => ({
    status: 200,
    json: async () => ({ build: "next", builtAt: new Date().toISOString() }),
  }));
  const flush = vi.fn(async () => {});
  const reload = vi.fn();
  const changed = vi.fn();
  const notice = createUpdateNotice({
    currentBuild: "current",
    fetch,
    storage,
    now: Date.now,
    visible: () => visible,
    setInterval: (fn, ms) => setInterval(fn, ms),
    clearInterval: (timer) =>
      clearInterval(timer as ReturnType<typeof setInterval>),
    changed,
    flush,
    reload,
  });
  const version = (build: string) =>
    fetch.mockResolvedValue({
      status: 200,
      json: async () => ({ build, builtAt: new Date().toISOString() }),
    });
  return {
    notice,
    fetch,
    storage,
    flush,
    reload,
    changed,
    version,
    hide: () => {
      visible = false;
    },
    show: () => {
      visible = true;
    },
  };
}
beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-09-11T14:22:00Z"));
});
afterEach(() => vi.useRealTimers());
it("ignores the same build and announces a new build once", async () => {
  const t = setup();
  t.version("current");
  await t.notice.onReturn();
  expect(t.notice.state.available).toBe(false);
  t.version("next");
  await vi.advanceTimersByTimeAsync(60_000);
  await t.notice.onReturn();
  expect(t.notice.state.available).toBe(true);
  await vi.advanceTimersByTimeAsync(60_000);
  await t.notice.onReturn();
  expect(t.changed).toHaveBeenCalledTimes(1);
  expect(t.fetch).toHaveBeenCalledWith("/version.json", { cache: "no-store" });
});
it("persists per-build dismissals and resurfaces a newer build", async () => {
  const t = setup();
  await t.notice.onReturn();
  t.notice.dismiss();
  expect(t.storage.setItem).toHaveBeenCalledWith(
    "sema:update-dismissed",
    '["next"]',
  );
  await vi.advanceTimersByTimeAsync(60_000);
  await t.notice.onReturn();
  expect(t.notice.state).toMatchObject({ available: false, dismissed: true });
  t.version("newer");
  await vi.advanceTimersByTimeAsync(60_000);
  await t.notice.onReturn();
  expect(t.notice.state).toMatchObject({ available: true, resurfaced: true });
  t.notice.dismiss();
  t.version("next");
  await vi.advanceTimersByTimeAsync(60_000);
  await t.notice.onReturn();
  expect(t.notice.state.available).toBe(false);
  const restored = setup('["next"]');
  await restored.notice.onReturn();
  expect(restored.notice.state.available).toBe(false);
});
it("throttles checks for 60 seconds", async () => {
  const t = setup();
  await t.notice.onReturn();
  await vi.advanceTimersByTimeAsync(59_999);
  await t.notice.onReturn();
  expect(t.fetch).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(1);
  await t.notice.onReturn();
  expect(t.fetch).toHaveBeenCalledTimes(2);
});
it("ignores network errors and non-200 responses", async () => {
  const t = setup();
  t.fetch.mockRejectedValueOnce(new Error("offline"));
  await t.notice.onReturn();
  expect(t.notice.state.available).toBe(false);
  await vi.advanceTimersByTimeAsync(60_000);
  t.fetch.mockResolvedValueOnce({
    status: 503,
    json: async () => ({ build: "next", builtAt: new Date().toISOString() }),
  });
  await t.notice.onReturn();
  expect(t.changed).not.toHaveBeenCalled();
});
it("checks only while visible and defers interval discoveries until return", async () => {
  const t = setup();
  t.hide();
  await vi.advanceTimersByTimeAsync(900_000);
  expect(t.fetch).not.toHaveBeenCalled();
  t.show();
  await vi.advanceTimersByTimeAsync(900_000);
  expect(t.fetch).toHaveBeenCalledTimes(1);
  expect(t.notice.state.available).toBe(false);
  await t.notice.onReturn();
  expect(t.notice.state.available).toBe(true);
  expect(t.fetch).toHaveBeenCalledTimes(1);
  t.notice.dispose();
  await vi.advanceTimersByTimeAsync(900_000);
  expect(t.fetch).toHaveBeenCalledTimes(1);
});
it("flushes before user-triggered reload and prevents repeated reloads", async () => {
  const t = setup();
  await t.notice.onReturn();
  expect(t.reload).not.toHaveBeenCalled();
  let finish: () => void = () => {};
  t.flush.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  const pending = t.notice.reload();
  expect(t.notice.state.reloading).toBe(true);
  expect(t.reload).not.toHaveBeenCalled();
  await t.notice.reload();
  expect(t.flush).toHaveBeenCalledTimes(1);
  finish();
  await pending;
  expect(t.reload).toHaveBeenCalledTimes(1);
});
it("formats deployment metadata", async () => {
  const t = setup();
  await t.notice.onReturn();
  expect(updateMeta(t.notice.state, Date.now() + 120_000)).toBe(
    "deployed 2 min ago · next",
  );
  expect(updateMeta(t.notice.state, Date.now() + 3_600_000)).toMatch(
    /^deployed at \d\d:\d\d · next$/,
  );
  expect(
    updateMeta({ ...t.notice.state, resurfaced: true }, Date.now()),
  ).toMatch(/^second deploy · \d\d:\d\d$/);
});
