export type Order = "chrono" | "interest";

export interface Profile {
  email: string;
  created_at: string;
  order_pref: Order;
}

export interface MeResponse {
  profile: Profile;
  signal_count: number;
}

export interface Item {
  item_id: string;
  feed_id: string;
  feed_title?: string;
  favicon_url?: string;
  url: string;
  title: string;
  summary?: string;
  author?: string;
  published_ts: string;
  fetched_ts: string;
  media_url?: string;
  media_w?: number;
  media_h?: number;
  body_url?: string;
  has_body: boolean;
  score: number;
  size: "S" | "M" | "L";
  read: boolean;
  signal: -1 | 0 | 1;
}

export interface ItemsResponse {
  items: Item[];
  next_cursor: string | null;
}

export interface Feed {
  feed_id: string;
  url: string;
  site_url?: string;
  title?: string;
  favicon_url?: string;
  last_fetch_at?: string;
  last_status?: string;
  error_count: number;
  next_fetch_at: string;
}
