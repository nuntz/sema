import type { Page } from "@playwright/test";

/** Stub the API boundary; production loading, player lifecycle and UI still run. */
export async function stubYouTube(
  page: Page,
  failure = false,
  controls = false,
) {
  if (controls)
    await page.route(
      "https://www.youtube-nocookie.com/sema-test-player",
      (route) =>
        route.fulfill({
          contentType: "text/html",
          body: `<button onclick="parent.postMessage({ state: 3 }, '*'); setTimeout(() => parent.postMessage({ state: 1 }, '*'), 20)">Seek</button><button onclick="parent.postMessage({ state: 2 }, '*')">Pause</button><button>Mute</button>`,
        }),
    );
  await page.addInitScript(
    ({ failure, controls }) => {
      window.YT = {
        Player: class {
          frame: HTMLIFrameElement;
          seconds = 0;
          options: ConstructorParameters<
            NonNullable<Window["YT"]>["Player"]
          >[1];
          constructor(
            host: HTMLElement,
            options: ConstructorParameters<
              NonNullable<Window["YT"]>["Player"]
            >[1],
          ) {
            this.options = options;
            this.frame = document.createElement("iframe");
            this.frame.title = "YouTube test player";
            this.frame.dataset.videoId = options.videoId;
            this.frame.dataset.host = options.host;
            if (controls) {
              this.frame.src =
                "https://www.youtube-nocookie.com/sema-test-player";
              window.addEventListener("message", (event) => {
                if (
                  event.source === this.frame.contentWindow &&
                  event.origin === "https://www.youtube-nocookie.com"
                ) {
                  this.frame.dataset.state = String(event.data.state);
                  options.events.onStateChange({ data: event.data.state });
                }
              });
            }
            host.replaceWith(this.frame);
            this.frame.addEventListener("test-state", (event) =>
              options.events.onStateChange({
                data: (event as CustomEvent<number>).detail,
              }),
            );
            queueMicrotask(() => {
              if (failure) options.events.onError();
              else options.events.onReady({ target: this });
            });
          }
          playVideo() {
            this.options.events.onStateChange({ data: 1 });
          }
          seekTo(seconds: number) {
            this.seconds = seconds;
            this.frame.dataset.seconds = String(seconds);
          }
          getCurrentTime() {
            return this.seconds || 87;
          }
          destroy() {
            this.frame.remove();
          }
        },
      };
    },
    { failure, controls },
  );
}
