import type {
  Feed,
  HeartResponse,
  ItemsResponse,
  MeResponse,
  Order,
  Profile,
} from "../types";

export interface FeedImportResult {
  imported: number;
  unsupported: Array<{ title?: string; url: string; reason: string }>;
}

export class UnauthorizedError extends Error {}

export class APIClient {
  constructor(private readonly token: () => string) {}

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers);
    headers.set("Authorization", `Bearer ${this.token()}`);
    if (init.body && !(init.body instanceof FormData))
      headers.set("Content-Type", "application/json");
    const response = await fetch(`/api${path}`, { ...init, headers });
    if (response.status === 401)
      throw new UnauthorizedError("Your Google session expired.");
    if (!response.ok) {
      const payload = await response
        .json()
        .catch(() => ({ error: response.statusText }));
      throw new Error(payload.error ?? `Request failed (${response.status})`);
    }
    if (response.status === 204) return undefined as T;
    return response.json() as Promise<T>;
  }

  me(): Promise<MeResponse> {
    return this.request("/me");
  }

  patchMe(
    patch: Partial<Pick<Profile, "order_pref">>,
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
  ): Promise<ItemsResponse> {
    const params = new URLSearchParams({ order, limit: "100" });
    if (cursor) params.set("cursor", cursor);
    if (includeRead) params.set("include_read", "true");
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

  feeds(): Promise<Feed[]> {
    return this.request<{ feeds: Feed[] }>("/feeds").then(
      (payload) => payload.feeds,
    );
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
