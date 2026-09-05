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

export interface MediaVariant {
  url: string;
  width: number;
  height: number;
}

export type Connector = "rss" | "youtube" | "reddit";
export type RedditPostType = "text" | "link" | "image" | "gallery" | "video";

export interface Item {
  item_id: string;
  story_id?: string;
  feed_id: string;
  feed_title?: string;
  connector?: Connector;
  favicon_url?: string;
  url: string;
  external_url?: string;
  post_type?: RedditPostType;
  title: string;
  summary?: string;
  summary_source: "feed" | "body" | "generated" | "";
  description?: string;
  author?: string;
  display_date?: string;
  published_ts: string;
  fetched_ts: string;
  media_url?: string;
  media_variants?: MediaVariant[];
  media_w?: number;
  media_h?: number;
  media_type?: "video" | string;
  video_id?: string;
  is_short?: boolean;
  body_url?: string;
  has_body: boolean;
  extract_quality: number;
  score: number;
  size: "S" | "M" | "L";
  why?: Why;
  read: boolean;
  signal: -1 | 0 | 1;
  hearted: boolean;
  archived?: boolean;
  hearted_ts?: string;
  similarity?: number;
}

export interface Story {
  story_id: string;
  source_count: number;
  items: Item[];
}

export interface StoriesResponse {
  stories: Story[];
}

export interface SearchGroup {
  window: Item[];
  archive: Item[];
}

export interface SearchResponse {
  matches: SearchGroup;
  related: SearchGroup;
  semantic_available: boolean;
}

export interface ItemsResponse {
  items: Item[];
  next_cursor: string | null;
  read_anchor?: ReadAnchor;
}

export interface ReadAnchor {
  item_id: string;
  published_ts: string;
}

export interface HeartResponse {
  archive_sk: string;
  heart_count: number;
}

export interface Feed {
  feed_id: string;
  url: string;
  connector: Connector;
  site_url?: string;
  title?: string;
  custom_title?: string;
  tags: string[];
  muted: boolean;
  hide_shorts: boolean;
  always_generate: boolean;
  fetch_interval_h: 1 | 3 | 6 | 24;
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
  extraction_success_rate?: number;
  average_extract_quality?: number;
  extraction_sample: number;
}

export interface FeedCandidate {
  feed_url: string;
  title: string;
  type: "rss" | "atom" | "json" | string;
  connector: Connector;
  site_url?: string;
  badge_url?: string;
  avatar_url?: string;
  cadence?: string;
  item_count: number;
  newest_item_ts?: string;
}
