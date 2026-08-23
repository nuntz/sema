export type Order = "chrono" | "interest";

export interface Profile {
  email: string;
  created_at: string;
  order_pref: Order;
  tag_pref?: string;
  heart_count: number;
}

export interface MeResponse {
  profile: Profile;
  signal_count: number;
  heart_count: number;
  model: RankingModel;
}

export interface RankingModel {
  explicit_count: number;
  liked_count: number;
  disliked_count: number;
  implicit_count: number;
  size_cutoffs?: { p60: number; p90: number };
  computed_at?: string;
  version?: string;
}

export interface Why {
  title?: string;
  feed_title?: string;
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
  why?: Why;
  read: boolean;
  signal: -1 | 0 | 1;
  hearted: boolean;
  hearted_ts?: string;
}

export interface ItemsResponse {
  items: Item[];
  next_cursor: string | null;
}

export interface HeartResponse {
  archive_sk: string;
  heart_count: number;
}

export interface Feed {
  feed_id: string;
  url: string;
  site_url?: string;
  title?: string;
  custom_title?: string;
  tags: string[];
  muted: boolean;
  fetch_interval_h: 1 | 6 | 24;
  favicon_url?: string;
  last_fetch_at?: string;
  last_status?: string;
  last_error?: string;
  error_count: number;
  next_fetch_at: string;
  prior: number;
  prior_signals: number;
  status: "ok" | "slowed" | "broken" | "muted";
  item_count: number;
}

export interface FeedCandidate {
  feed_url: string;
  title: string;
  type: "rss" | "atom" | "json" | string;
  item_count: number;
  newest_item_ts?: string;
}
