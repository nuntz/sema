import { createSignal, Show, useContext } from "solid-js";
import { connectorKind } from "../reddit-item";
import { ImageLoadingEnabledContext } from "./image-loading";

export type BadgeSize = 12 | 16 | 20 | 22 | 28 | 32 | 36;

export function SourceBadge(props: {
  connector?: string;
  imageURL?: string;
  title?: string;
  size: BadgeSize;
  class?: string;
}) {
  const imagesEnabled = useContext(ImageLoadingEnabledContext);
  const [failed, setFailed] = createSignal(false);
  const connector = () => connectorKind(props.connector);
  const channel = () => connector() === "youtube";
  const reddit = () => connector() === "reddit";
  const initials = () => {
    if (reddit()) {
      const subreddit = (props.title || "Reddit").trim().replace(/^r\//i, "");
      return `r/${(subreddit[0] || "r").toLowerCase()}`;
    }
    const words = (props.title || "Feed").trim().split(/\s+/).filter(Boolean);
    return words
      .slice(0, 2)
      .map((word) => word[0]?.toUpperCase())
      .join("");
  };
  const showImage = () =>
    Boolean(props.imageURL) &&
    !failed() &&
    (channel() || reddit() || props.size <= 22);

  return (
    <span
      class={`source-badge badge-${props.size} ${channel() ? "channel" : "site"} ${props.class ?? ""}`}
      aria-hidden="true"
    >
      <Show
        when={showImage()}
        fallback={<span class="source-badge-fallback">{initials()}</span>}
      >
        <img
          src={imagesEnabled ? props.imageURL : undefined}
          alt=""
          onError={() => setFailed(true)}
        />
      </Show>
    </span>
  );
}
