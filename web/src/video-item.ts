import type { Item } from "./types";

const VIDEO_ID = /^[\w-]{11}$/;

export function youtubeVideoID(value: string): string | undefined {
  try {
    const url = new URL(value);
    if (url.protocol !== "https:" && url.protocol !== "http:") return;
    const host = url.hostname.toLowerCase().replace(/^www\./, "");
    const parts = url.pathname.split("/").filter(Boolean);
    const id =
      host === "youtu.be"
        ? parts[0]
        : [
              "youtube.com",
              "m.youtube.com",
              "music.youtube.com",
              "youtube-nocookie.com",
            ].includes(host)
          ? url.pathname === "/watch"
            ? url.searchParams.get("v")
            : ["shorts", "live", "embed"].includes(parts[0])
              ? parts[1]
              : undefined
          : undefined;
    return id && VIDEO_ID.test(id) ? id : undefined;
  } catch {
    return undefined;
  }
}

export function videoItemID(
  item: Pick<Item, "video_id" | "url" | "connector" | "external_url">,
): string | undefined {
  return (
    (item.video_id && VIDEO_ID.test(item.video_id)
      ? item.video_id
      : undefined) ||
    (item.connector === "reddit"
      ? youtubeVideoID(item.external_url || "")
      : undefined) ||
    youtubeVideoID(item.url)
  );
}

export const isVideoItem = (item: Parameters<typeof videoItemID>[0]): boolean =>
  !!videoItemID(item);
export const videoItemURL = (item: Parameters<typeof videoItemID>[0]): string =>
  `https://www.youtube.com/watch?v=${videoItemID(item)}`;
