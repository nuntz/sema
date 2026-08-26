import { createSignal, onCleanup, onMount } from "solid-js";

export function createMediaQuery(query: string): () => boolean {
  const [matches, setMatches] = createSignal(false);

  onMount(() => {
    const media = window.matchMedia(query);
    const update = () => setMatches(media.matches);
    update();
    media.addEventListener("change", update);
    onCleanup(() => media.removeEventListener("change", update));
  });

  return matches;
}
