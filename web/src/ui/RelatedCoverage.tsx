import { Show } from "solid-js";
import { Icon } from "../components/Icon";
import {
  gridSourceName,
  headlineText,
  repeatsLeadHeadline,
} from "../grid-display";
import type { Item } from "../types";

export function RelatedCoverage(props: {
  lead: Item;
  item: Item;
  age: string;
}) {
  return (
    <>
      <Icon name="stack" size={13} />
      <span class="story-related-copy">
        <Show
          when={repeatsLeadHeadline(props.lead.title, props.item.title)}
          fallback={
            <>
              <span class="story-related-source">
                {gridSourceName(props.item)} · {props.age}
              </span>
              <span class="story-related-title">
                {headlineText(props.item.title)}
              </span>
            </>
          }
        >
          <span class="story-related-source">
            Also covered by <b>{gridSourceName(props.item)}</b> · {props.age}
          </span>
        </Show>
      </span>
    </>
  );
}
