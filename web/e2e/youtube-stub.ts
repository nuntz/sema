import type { Page } from "@playwright/test";

/** Serve the player protocol from its real cross-origin iframe boundary. */
export async function stubYouTube(
  page: Page,
  failure = false,
  controls = false,
) {
  await page.route("https://www.youtube-nocookie.com/embed/*", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: `${controls ? '<button onclick="seek()">Seek</button><button onclick="state(2)">Pause</button><button>Mute</button>' : ""}<script>
      const params = new URLSearchParams(location.search);
      const origin = params.get('origin');
      let seconds = Number(params.get('start')) || 87;
      const send = message => parent.postMessage(JSON.stringify(message), origin);
      const state = playerState => send({event:'infoDelivery', info:{playerState, currentTime:seconds}});
      const seek = () => { state(3); setTimeout(() => state(1), 20); };
      addEventListener('message', event => {
        if (event.source !== parent || event.origin !== origin) return;
        let message; try { message = JSON.parse(event.data); } catch { return; }
        if (message.event === 'listening') {
          if (${failure}) send({event:'onError', info:150});
          else { send({event:'onReady'}); state(1); }
        } else if (message.event === 'command') {
          if (message.func === 'playVideo') state(1);
          if (message.func === 'pauseVideo') state(2);
          if (message.func === 'seekTo') { seconds = message.args[0]; seek(); }
        }
      });
    </script>`,
    }),
  );
}
