import type { Locator, Page } from "@playwright/test";

/**
 * Append every state the player reports from now on to its `data-state-log`,
 * in order. `data-state` lags the command that changes it, so right after a
 * key it can't show whether the key sent one; replies come in order, so a
 * command the key sent logs its state before a later command's. The stub
 * reports unstarted (-1, no data-state) until the app's autoplay, so once
 * data-state first reads "1" no startup reply is still on its way.
 */
export async function logPlayerStates(player: Locator) {
  await player.evaluate((iframe: HTMLIFrameElement) => {
    new MutationObserver(() => {
      const log = iframe.dataset.stateLog;
      iframe.dataset.stateLog = `${log ? `${log},` : ""}${iframe.dataset.state}`;
    }).observe(iframe, { attributeFilter: ["data-state"] });
  });
}

/**
 * Post the player a seek from the page's window, the one the app posts its
 * commands from. One window's messages reach the frame in order, so the seek's
 * buffering (3) logs after the reply to every command the app sent before it.
 */
export async function seekPlayer(player: Locator, seconds: number) {
  await player.evaluate((iframe: HTMLIFrameElement, seconds) => {
    iframe.contentWindow?.postMessage(
      JSON.stringify({
        event: "command",
        func: "seekTo",
        args: [seconds, true],
      }),
      new URL(iframe.src).origin,
    );
  }, seconds);
}

/**
 * Serve the player protocol from its real cross-origin iframe boundary.
 * Playing is reported 400ms after the app's playVideo, as a real player
 * buffers first, so a test that acts before its player plays, such as Escape
 * on a Peek that can only Dock a live video, fails on every run rather than
 * on a busy CI runner (#68).
 */
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
          else { send({event:'onReady'}); state(-1); }
        } else if (message.event === 'command') {
          if (message.func === 'playVideo') setTimeout(() => state(1), 400);
          if (message.func === 'pauseVideo') state(2);
          if (message.func === 'seekTo') { seconds = message.args[0]; seek(); }
        }
      });
    </script>`,
    }),
  );
}
