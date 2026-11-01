# Video playback survives surface changes through one fixed iframe host

The YouTube player is a cross-origin iframe, and browsers reload an iframe that is moved to a new parent. So a video only keeps playing across a surface change if a single fixed host stays put in the DOM and is positioned over whichever slot currently shows the video. Sema cannot reparent the player.

The Dock and the Peek are both fixed frames, so a hand-off between them just moves the host, and playback continues without a break. The reader lead scrolls inside the article. Keeping the host on it would mean tracking the slot every frame, so hand-offs between the reader and the Dock reload the player with a seek instead, as Flip does.

## Dock behaviour

- A video Docks automatically when its Peek or reader goes away while it is playing or buffering.
- At most one Dock exists, and a new Play replaces it.
- The Dock is not an Overlay and never changes Read state.
- Dwell keeps accruing while Docked. A Dock that started from a Peek counts dwell from zero.
- The Dock's controls are a strip above the video holding the Item title, Expand and close. The strip exists because the iframe swallows taps.
