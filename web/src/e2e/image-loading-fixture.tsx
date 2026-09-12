import { render } from "solid-js/web";
import type { Item } from "../types";
import { ResponsiveImage } from "../ui/ResponsiveImage";

const root = document.getElementById("root");
if (!root) throw new Error("Missing fixture root");

// A 400px scroller has 100px image overscan. Start outside it, then let
// the test move the image exactly onto the observer's expanded bottom edge.
render(
  () => (
    <div
      class="grid-scroll"
      style="position:fixed;top:0;left:0;width:400px;height:400px;overflow:auto"
    >
      <ResponsiveImage
        item={{ media_url: "/sema-mark.svg" } as Item}
        alt="Boundary image"
        deferUntilVisible
        loading="eager"
        style="position:absolute;top:501px;left:0;width:100px;height:100px"
      />
    </div>
  ),
  root,
);
