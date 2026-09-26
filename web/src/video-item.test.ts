import { describe, expect, it } from "vitest";
import { isVideoItem, videoItemID, youtubeVideoID } from "./video-item";

const id = "dQw4w9WgXcQ";
describe("Video Items", () => {
  it.each([
    `https://www.youtube.com/watch?v=${id}&t=12s`,
    `https://m.youtube.com/watch?v=${id}`,
    `https://youtu.be/${id}?t=10`,
    `https://youtube.com/shorts/${id}`,
    `https://youtube.com/live/${id}`,
    `https://youtube.com/embed/${id}`,
    `https://www.youtube-nocookie.com/embed/${id}`,
  ])("derives a video ID from %s", (url) =>
    expect(youtubeVideoID(url)).toBe(id),
  );
  it.each([
    "not a URL",
    `https://youtube.com.evil.test/watch?v=${id}`,
    "https://youtube.com/watch?v=bad",
    "https://youtube.com/@channel",
    `ftp://youtu.be/${id}`,
    `https://example.com/${id}`,
  ])("rejects %s", (url) => expect(youtubeVideoID(url)).toBeUndefined());
  it("uses stored IDs or links, never media_type or links in the body", () => {
    const item = {
      url: "https://example.com",
      media_type: "video",
      description: `https://youtu.be/${id}`,
    };
    expect(isVideoItem(item)).toBe(false);
    expect(videoItemID({ ...item, video_id: id })).toBe(id);
    expect(
      videoItemID({
        ...item,
        connector: "reddit",
        external_url: `https://youtu.be/${id}`,
      }),
    ).toBe(id);
    expect(
      videoItemID({
        ...item,
        connector: "rss",
        external_url: `https://youtu.be/${id}`,
      }),
    ).toBeUndefined();
  });
});
