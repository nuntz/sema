import type {
  Feed,
  FeedCandidate,
  HeartResponse,
  ItemsResponse,
  MeResponse,
  Order,
  Profile,
  SearchResponse,
} from "../types";

export interface FeedImportResult {
  imported: number;
  unsupported: Array<{ title?: string; url: string; reason: string }>;
}

export class UnauthorizedError extends Error {}

export class APIError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly kind?: string,
    readonly upstreamStatus?: number,
  ) {
    super(message);
    this.name = "APIError";
  }
}

let sessionBootstrap: MeResponse | undefined;

export function primeSessionBootstrap(value: MeResponse): void {
  sessionBootstrap = value;
}

export function clearSessionBootstrap(): void {
  sessionBootstrap = undefined;
}

export class APIClient {
  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers);
    if (init.body && !(init.body instanceof FormData))
      headers.set("Content-Type", "application/json");
    const response = await fetch(`/api${path}`, { ...init, headers });
    if (response.status === 401)
      throw new UnauthorizedError("Your Sema session expired.");
    if (!response.ok) {
      const payload = await response
        .json()
        .catch(() => ({ error: response.statusText }));
      throw new APIError(
        payload.error ?? `Request failed (${response.status})`,
        response.status,
        payload.kind,
        payload.upstream_status,
      );
    }
    if (response.status === 204) return undefined as T;
    return response.json() as Promise<T>;
  }

  me(): Promise<MeResponse> {
    if (sessionBootstrap) {
      const value = sessionBootstrap;
      sessionBootstrap = undefined;
      return Promise.resolve(value);
    }
    return this.request("/me");
  }

  patchMe(
    patch: Partial<Pick<Profile, "order_pref" | "tag_pref">>,
    keepalive = false,
  ): Promise<void> {
    return this.request("/me", {
      method: "PATCH",
      body: JSON.stringify(patch),
      keepalive,
    });
  }

  items(
    order: Order,
    cursor = "",
    includeRead = false,
    tag = "",
  ): Promise<ItemsResponse> {
    const params = new URLSearchParams({ order, limit: "100" });
    if (cursor) params.set("cursor", cursor);
    if (includeRead) params.set("include_read", "true");
    if (tag) params.set("tag", tag === "untagged" ? "__untagged" : tag);
    return this.request(`/items?${params}`);
  }

  item(itemID: string) {
    return this.request<import("../types").Item>(
      `/items/${encodeURIComponent(itemID)}`,
    );
  }

  archive(cursor = ""): Promise<ItemsResponse> {
    const params = new URLSearchParams({ limit: "100" });
    if (cursor) params.set("cursor", cursor);
    return this.request(`/archive?${params}`);
  }

  archiveItem(itemID: string) {
    return this.request<import("../types").Item>(
      `/archive/${encodeURIComponent(itemID)}`,
    );
  }

  search(query: string, limit = 30): Promise<SearchResponse> {
    const params = new URLSearchParams({ q: query, limit: String(limit) });
    return this.request(`/search?${params}`);
  }

  similar(itemID: string, limit = 12): Promise<ItemsResponse> {
    const params = new URLSearchParams({ limit: String(limit) });
    return this.request(
      `/items/${encodeURIComponent(itemID)}/similar?${params}`,
    );
  }

  heart(itemID: string, hearted: boolean): Promise<HeartResponse> {
    return this.request(`/items/${encodeURIComponent(itemID)}/heart`, {
      method: "POST",
      body: JSON.stringify({ hearted }),
    });
  }

  signal(itemID: string, value: -1 | 0 | 1): Promise<void> {
    return this.request(`/items/${encodeURIComponent(itemID)}/signal`, {
      method: "POST",
      body: JSON.stringify({ value }),
    });
  }

  read(itemID: string, read: boolean): Promise<void> {
    return this.request(`/items/${encodeURIComponent(itemID)}/read`, {
      method: "POST",
      body: JSON.stringify({ read }),
    });
  }

  readBatch(ids: string[], read = true, keepalive = false): Promise<void> {
    return this.request("/items/read-batch", {
      method: "POST",
      body: JSON.stringify({ ids, read }),
      keepalive,
    });
  }

  events(
    itemID: string,
    event: {
      opened?: true;
      dwell_ms?: number;
      clicked_through?: true;
      shared?: true;
    },
    keepalive = false,
  ): Promise<void> {
    return this.request(`/items/${encodeURIComponent(itemID)}/events`, {
      method: "POST",
      body: JSON.stringify(event),
      keepalive,
    });
  }

  retryItem(itemID: string): Promise<{ queued: boolean }> {
    return this.request(`/items/${encodeURIComponent(itemID)}/retry`, {
      method: "POST",
    });
  }

  recomputeRanking(): Promise<{
    model: import("../types").RankingModel;
    items_rescored?: number;
  }> {
    return this.request("/ranking/recompute", { method: "POST" });
  }

  feeds(): Promise<Feed[]> {
    return this.request<{ feeds: Feed[] }>("/feeds").then(
      (payload) => payload.feeds,
    );
  }

  discoverFeed(url: string): Promise<FeedCandidate[]> {
    return this.request<{ candidates: FeedCandidate[] }>("/feeds/discover", {
      method: "POST",
      body: JSON.stringify({ url }),
    }).then((payload) => payload.candidates);
  }

  addFeed(input: {
    feed_url: string;
    tags?: string[];
    custom_title?: string;
    connector?: string;
    title?: string;
    site_url?: string;
    badge_url?: string;
    avatar_url?: string;
  }): Promise<{ feed: Feed; created: boolean }> {
    return this.request("/feeds", {
      method: "POST",
      body: JSON.stringify(input),
    });
  }

  patchFeed(
    feedID: string,
    patch: Partial<
      Pick<
        Feed,
        | "custom_title"
        | "tags"
        | "muted"
        | "hide_shorts"
        | "always_generate"
        | "fetch_interval_h"
        | "url"
      >
    >,
  ): Promise<Feed> {
    return this.request(`/feeds/${encodeURIComponent(feedID)}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
  }

  retryFeed(feedID: string): Promise<Feed> {
    return this.request(`/feeds/${encodeURIComponent(feedID)}/retry`, {
      method: "POST",
    });
  }

  async exportOPML(): Promise<Blob> {
    const response = await fetch("/api/feeds/export.opml");
    if (response.status === 401)
      throw new UnauthorizedError("Your Sema session expired.");
    if (!response.ok) throw new Error(`Export failed (${response.status})`);
    return response.blob();
  }

  importOPML(file: File): Promise<FeedImportResult> {
    const data = new FormData();
    data.append("file", file);
    return this.request("/feeds/import", { method: "POST", body: data });
  }

  deleteFeed(feedID: string): Promise<void> {
    return this.request(`/feeds/${encodeURIComponent(feedID)}`, {
      method: "DELETE",
    });
  }
}
