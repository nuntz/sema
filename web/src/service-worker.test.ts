import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { describe, expect, it, vi } from "vitest";

type FetchEvent = {
  request: { method: string; url: string };
  respondWith: (response: Promise<unknown>) => void;
};

const serviceWorker = readFileSync(
  new URL("../public/sw.js", import.meta.url),
  "utf8",
);

const runtime = () => {
  let fetchHandler: ((event: FetchEvent) => void) | undefined;
  let installHandler:
    | ((event: { waitUntil: (promise: Promise<unknown>) => void }) => void)
    | undefined;
  const addAll = vi.fn();
  const put = vi.fn();
  const caches = {
    open: vi.fn(async () => ({ addAll, put })),
    keys: vi.fn(async () => []),
    delete: vi.fn(),
    match: vi.fn(),
  };
  const fetch = vi.fn(async () => ({ ok: true, clone: () => ({}) }));
  const self = {
    location: { origin: "https://reader.example" },
    clients: { claim: vi.fn() },
    skipWaiting: vi.fn(),
    addEventListener: (name: string, handler: unknown) => {
      if (name === "install") installHandler = handler as typeof installHandler;
      if (name === "fetch")
        fetchHandler = handler as (event: FetchEvent) => void;
    },
  };
  runInNewContext(serviceWorker, { self, caches, fetch, URL, Promise });
  if (!fetchHandler)
    throw new Error("service worker did not register a fetch handler");
  return { fetchHandler, put, installHandler, addAll };
};

describe("service worker runtime caching", () => {
  it.each([
    ["/index.html", true],
    ["/assets/app-a1b2c3.js", true],
    ["/feed.xml", false],
    ["/version.json", false],
  ])("caches %s only when it is shell content", async (path, cacheable) => {
    const { fetchHandler, put } = runtime();
    let response: Promise<unknown> | undefined;
    fetchHandler({
      request: { method: "GET", url: `https://reader.example${path}` },
      respondWith: (value) => {
        response = value;
      },
    });
    await response;
    await Promise.resolve();
    expect(put).toHaveBeenCalledTimes(cacheable ? 1 : 0);
  });
});

it("does not precache version metadata during installation", async () => {
  const { installHandler, addAll } = runtime();
  let pending: Promise<unknown> | undefined;
  installHandler?.({
    waitUntil: (promise) => {
      pending = promise;
    },
  });
  await pending;
  expect(addAll).toHaveBeenCalledOnce();
  expect(addAll.mock.calls[0][0]).not.toContain("/version.json");
});
