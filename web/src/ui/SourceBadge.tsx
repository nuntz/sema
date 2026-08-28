import { createSignal, Show } from "solid-js";
import { connectorKind } from "../reddit-item";

export type BadgeSize = 16 | 20 | 28 | 32 | 36;

export function SourceBadge(props: {
  connector?: string;
  imageURL?: string;
  title?: string;
  size: BadgeSize;
  class?: string;
}) {
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
    (channel() || reddit() || props.size <= 20);

  return (
    <span
      class={`source-badge badge-${props.size} ${channel() ? "channel" : "site"} ${props.class ?? ""}`}
      aria-hidden="true"
    >
      <Show
        when={showImage()}
        fallback={<span class="source-badge-fallback">{initials()}</span>}
      >
        <img src={props.imageURL} alt="" onError={() => setFailed(true)} />
      </Show>
    </span>
  );
}
