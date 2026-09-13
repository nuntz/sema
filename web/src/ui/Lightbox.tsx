import {
  createEffect,
  createSignal,
  For,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import { Portal } from "solid-js/web";
import { Icon } from "../components/Icon";
import { createMediaQuery } from "../media-query";
import { imageIndex, lightboxBindings, lightboxCommand } from "./lightbox-keys";
import { fittedRect, type LightboxImage, originalSource } from "./lightbox-set";
import { closeOverlay, pushOverlay } from "./overlay-history";
import {
  beginSheetDrag,
  lightboxDragOffset,
  lightboxDragStyle,
  shouldDismissLightbox,
} from "./sheet-drag";

export function Lightbox(props: {
  images: LightboxImage[];
  initialIndex: number;
  onClose(): void;
  onOpenReader?(): void;
}) {
  const phone = createMediaQuery("(max-width: 619px)");
  const reduced = createMediaQuery("(prefers-reduced-motion: reduce)");
  const origin = props.images[props.initialIndex].element;
  const [index, setIndex] = createSignal(props.initialIndex);
  const current = () => props.images[index()];
  const [rect, setRect] = createSignal(
    fittedRect(current(), innerWidth, innerHeight),
  );
  const [zoom, setZoom] = createSignal(1);
  const [native, setNative] = createSignal(1);
  const [pan, setPan] = createSignal({ x: 0, y: 0 });
  const [travel, setTravel] = createSignal(0);
  const [down, setDown] = createSignal(0);
  const [idle, setIdle] = createSignal(false);
  const [mapVisible, setMapVisible] = createSignal(true);
  const [help, setHelp] = createSignal(false);
  const [previousCaption, setPreviousCaption] = createSignal("");
  const [loading, setLoading] = createSignal(false);
  const [sharp, setSharp] = createSignal("");
  const [failed, setFailed] = createSignal(false);
  const [closing, setClosing] = createSignal(false);
  const [dragging, setDragging] = createSignal(false);
  let dialog!: HTMLDivElement;
  let frame!: HTMLDivElement;
  let scrim!: HTMLDivElement;
  let closeButton!: HTMLButtonElement;
  let idleTimer = 0;
  let mapTimer = 0;
  let closeTimer = 0;
  let swipeTimer = 0;
  let captionTimer = 0;
  let lastTap = 0;
  let gesture:
    | {
        x: number;
        y: number;
        time: number;
        pan: { x: number; y: number };
        axis?: "x" | "y";
        pinched?: boolean;
      }
    | undefined;
  const pointers = new Map<number, { x: number; y: number }>();
  let pinchDistance = 0;
  let pinchZoom = 1;
  const caption = () => current().caption || current().alt;
  const shownIndex = () =>
    imageIndex(
      index(),
      Math.abs(travel()) >= (innerWidth + 24) / 2 ? (travel() < 0 ? 1 : -1) : 0,
      props.images.length,
    );
  const wake = () => {
    setIdle(false);
    clearTimeout(idleTimer);
    idleTimer = window.setTimeout(() => setIdle(true), 3000);
  };
  const showMap = () => {
    setMapVisible(true);
    clearTimeout(mapTimer);
    mapTimer = window.setTimeout(() => setMapVisible(false), 1200);
  };
  const constrainPan = (x: number, y: number, scale = zoom()) => {
    const maxX = Math.max(0, (rect().width * scale - innerWidth) / 2);
    const maxY = Math.max(0, (rect().height * scale - innerHeight) / 2);
    setPan({
      x: Math.max(-maxX, Math.min(maxX, x)),
      y: Math.max(-maxY, Math.min(maxY, y)),
    });
    showMap();
  };
  const setScale = (scale: number, x = innerWidth / 2, y = innerHeight / 2) => {
    if (native() <= 1) return;
    const next = Math.max(1, Math.min(phone() ? 6 : native(), scale));
    const ratio = next / zoom();
    const previous = pan();
    setZoom(next);
    constrainPan(
      (previous.x - x + innerWidth / 2) * ratio + x - innerWidth / 2,
      (previous.y - y + innerHeight / 2) * ratio + y - innerHeight / 2,
      next,
    );
    wake();
  };
  const toggleZoom = () => {
    if (native() > 1) setScale(zoom() > 1 ? 1 : phone() ? 2 : native());
  };
  const openOriginal = () =>
    window.open(current().src, "_blank", "noopener,noreferrer");
  const close = (dismiss = false) => {
    if (closing()) return;
    setClosing(true);
    const duration = reduced() ? (dismiss ? 0 : 90) : dismiss ? 200 : 140;
    const destination = origin.getBoundingClientRect();
    const visible = frame.getBoundingClientRect();
    const startTransform = `translate(${visible.left - rect().left}px, ${visible.top - rect().top}px) scale(${visible.width / rect().width}, ${visible.height / rect().height})`;
    frame.animate(
      reduced()
        ? [{ opacity: 1 }, { opacity: 0 }]
        : [
            { transform: startTransform, transformOrigin: "0 0", opacity: 1 },
            {
              transform: `translate(${destination.left - rect().left}px, ${destination.top - rect().top}px) scale(${destination.width / rect().width}, ${destination.height / rect().height})`,
              transformOrigin: "0 0",
              borderRadius: "4px",
              opacity: destination.width && visible.width ? 1 : 0,
            },
          ],
      { duration, easing: "cubic-bezier(.2,.8,.25,1)", fill: "forwards" },
    );
    scrim.animate(
      [{ opacity: getComputedStyle(scrim).opacity }, { opacity: 0 }],
      { duration, fill: "forwards" },
    );
    closeOverlay("lightbox");
    closeTimer = window.setTimeout(props.onClose, duration);
  };
  const openReader = () => {
    if (!props.onOpenReader || closing()) return;
    close();
    props.onOpenReader();
  };
  const navigate = (next: number) => {
    if (closing()) return;
    clearTimeout(swipeTimer);
    const target = imageIndex(next, 0, props.images.length);
    if (target === index()) {
      if (!reduced() && props.images.length > 1)
        frame.animate(
          [
            { translate: "0px" },
            { translate: `${next < index() ? 16 : -16}px` },
            { translate: "0px" },
          ],
          { duration: 160, easing: "cubic-bezier(.2,.8,.25,1)" },
        );
      return;
    }
    clearTimeout(captionTimer);
    setPreviousCaption(reduced() ? "" : caption());
    captionTimer = window.setTimeout(() => setPreviousCaption(""), 120);
    gesture = undefined;
    pointers.clear();
    setDragging(false);
    setZoom(1);
    setPan({ x: 0, y: 0 });
    setTravel(0);
    setDown(0);
    setRect(fittedRect(props.images[target], innerWidth, innerHeight));
    setIndex(target);
    wake();
  };
  createEffect(() => {
    const member = current();
    let cancelled = false;
    let timeout = 0;
    setSharp("");
    setFailed(false);
    const naturalWidth = member.element.naturalWidth;
    setNative(Math.max(1, naturalWidth / rect().width));
    const prefetch = () => {
      for (const neighbour of [
        props.images[index() - 1],
        props.images[index() + 1],
      ]) {
        if (neighbour) {
          const image = new Image();
          image.src = originalSource(neighbour);
        }
      }
    };
    if (member.variants) {
      setLoading(true);
      const image = new Image();
      const settle = (ok: boolean) => {
        if (cancelled) return;
        cancelled = true;
        clearTimeout(timeout);
        setLoading(false);
        if (ok) {
          setSharp(image.src);
          setNative(Math.max(1, image.naturalWidth / rect().width));
        }
        prefetch();
      };
      image.onload = () => {
        void image.decode().then(
          () => settle(true),
          () => settle(false),
        );
      };
      image.onerror = () => settle(false);
      timeout = window.setTimeout(() => settle(false), 8000);
      image.src = originalSource(member);
      onCleanup(() => {
        image.onload = null;
        image.onerror = null;
      });
    } else {
      setLoading(false);
      if (!naturalWidth) {
        void member.element.decode().then(
          () => {
            if (!cancelled) {
              // Detached grid images may initially have only fallback geometry.
              if (
                !member.element.isConnected &&
                !(member.width && member.height)
              )
                setRect(fittedRect(member, innerWidth, innerHeight));
              setNative(
                Math.max(1, member.element.naturalWidth / rect().width),
              );
              prefetch();
            }
          },
          () => {
            if (!cancelled) {
              setFailed(true);
              prefetch();
            }
          },
        );
      } else prefetch();
    }
    onCleanup(() => {
      cancelled = true;
      clearTimeout(timeout);
    });
  });
  const onKey = (event: KeyboardEvent) => {
    if (
      event.defaultPrevented ||
      event.isComposing ||
      event.metaKey ||
      event.ctrlKey ||
      event.altKey
    )
      return;
    event.preventDefault();
    event.stopImmediatePropagation();
    if (closing()) return;
    wake();
    switch (lightboxCommand(event.key)) {
      case "close":
        close();
        break;
      case "next":
        navigate(index() + 1);
        break;
      case "previous":
        navigate(index() - 1);
        break;
      case "first":
        navigate(0);
        break;
      case "last":
        navigate(props.images.length - 1);
        break;
      case "zoom":
        toggleZoom();
        break;
      case "reader":
        openReader();
        break;
      case "original":
        openOriginal();
        break;
      case "help":
        setHelp(!help());
        break;
      case "tab": {
        const controls = Array.from(
          dialog.querySelectorAll<HTMLButtonElement>("button"),
        ).filter(
          (button) =>
            button.getClientRects().length &&
            button.tabIndex >= 0 &&
            !button.disabled,
        );
        const position = controls.indexOf(
          document.activeElement as HTMLButtonElement,
        );
        controls[
          (position + (event.shiftKey ? -1 : 1) + controls.length) %
            controls.length
        ]?.focus();
        break;
      }
    }
  };
  onMount(() => {
    const siblings = Array.from(document.body.children).filter(
      (element): element is HTMLElement =>
        element instanceof HTMLElement && !element.contains(dialog),
    );
    const inert = siblings.map((element) => element.inert);
    siblings.forEach((element) => {
      element.inert = true;
    });
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    pushOverlay("lightbox", () => close());
    // Own modal keys before background controls (including tag filters).
    window.addEventListener("keydown", onKey, true);
    const keepFocus = (event: FocusEvent) => {
      if (!dialog.contains(event.target as Node)) closeButton.focus();
    };
    document.addEventListener("focusin", keepFocus);
    closeButton.focus();
    wake();
    const from = origin.getBoundingClientRect();
    const to = rect();
    frame.animate(
      reduced()
        ? [{ opacity: 0 }, { opacity: 1 }]
        : [
            {
              transform: `translate(${from.left - to.left}px, ${from.top - to.top}px) scale(${from.width / to.width}, ${from.height / to.height})`,
              transformOrigin: "0 0",
              borderRadius: "4px",
            },
            {
              transform: "none",
              transformOrigin: "0 0",
              borderRadius: "0px",
            },
          ],
      { duration: reduced() ? 90 : 180, easing: "cubic-bezier(.2,.8,.25,1)" },
    );
    scrim.animate([{ opacity: 0 }, { opacity: 1 }], {
      duration: reduced() ? 90 : 180,
    });
    onCleanup(() => {
      window.removeEventListener("keydown", onKey, true);
      document.removeEventListener("focusin", keepFocus);
      closeOverlay("lightbox");
      clearTimeout(idleTimer);
      clearTimeout(mapTimer);
      clearTimeout(closeTimer);
      clearTimeout(swipeTimer);
      clearTimeout(captionTimer);
      siblings.forEach((element, i) => {
        element.inert = inert[i];
      });
      document.body.style.overflow = overflow;
      if (origin.isConnected) origin.focus({ preventScroll: true });
    });
  });
  const pointerDown = (event: PointerEvent) => {
    if ((event.target as HTMLElement).closest("button, .lb-help") || closing())
      return;
    pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
    dialog.setPointerCapture(event.pointerId);
    if (pointers.size === 2 && phone()) {
      const [a, b] = [...pointers.values()];
      pinchDistance = Math.hypot(a.x - b.x, a.y - b.y);
      pinchZoom = zoom();
      if (gesture) gesture.pinched = true;
      setDown(0);
      setTravel(0);
    } else
      gesture = {
        x: event.clientX,
        y: event.clientY,
        time: performance.now(),
        pan: pan(),
      };
    setDragging(true);
  };
  const pointerMove = (event: PointerEvent) => {
    if (!phone()) wake();
    if (!pointers.has(event.pointerId) || !gesture) return;
    pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
    if (pointers.size === 2 && phone()) {
      const [a, b] = [...pointers.values()];
      setScale(
        (pinchZoom * Math.hypot(a.x - b.x, a.y - b.y)) /
          Math.max(1, pinchDistance),
        (a.x + b.x) / 2,
        (a.y + b.y) / 2,
      );
      return;
    }
    if (gesture.pinched) return;
    const x = event.clientX - gesture.x;
    const y = event.clientY - gesture.y;
    if (zoom() > 1) {
      constrainPan(gesture.pan.x + x, gesture.pan.y + y);
      return;
    }
    if (!phone()) return;
    if (!gesture.axis && Math.max(Math.abs(x), Math.abs(y)) > 8)
      gesture.axis = Math.abs(x) > Math.abs(y) ? "x" : "y";
    if (gesture.axis === "x") {
      const atEnd =
        (index() === 0 && x > 0) ||
        (index() === props.images.length - 1 && x < 0);
      setTravel(atEnd ? Math.sign(x) * Math.min(16, Math.abs(x) / 5) : x);
      setIdle(true);
    } else if (gesture.axis === "y") {
      const drag = beginSheetDrag("touch", gesture.y, gesture.time, 0);
      if (drag) setDown(lightboxDragOffset(drag, event.clientY));
      if (down()) setIdle(true);
    }
  };
  const pointerEnd = (event: PointerEvent) => {
    pointers.delete(event.pointerId);
    if (pointers.size) return;
    setDragging(false);
    if (!gesture) return;
    const previous = gesture;
    gesture = undefined;
    if (event.type === "pointercancel") {
      setTravel(0);
      setDown(0);
      wake();
      return;
    }
    if (previous.pinched) return;
    if (zoom() === 1 && previous.axis === "y") {
      const drag = beginSheetDrag("touch", previous.y, previous.time, 0);
      if (
        drag &&
        shouldDismissLightbox(drag, event.clientY, performance.now())
      ) {
        close(true);
        return;
      }
      setDown(0);
      wake();
    } else if (zoom() === 1 && previous.axis === "x") {
      const distance = event.clientX - previous.x;
      const next = imageIndex(
        index(),
        Math.abs(distance) >= (innerWidth + 24) / 2 ||
          Math.abs(distance) / Math.max(1, performance.now() - previous.time) >
            0.5
          ? distance < 0
            ? 1
            : -1
          : 0,
        props.images.length,
      );
      setTravel((index() - next) * (innerWidth + 24));
      swipeTimer = window.setTimeout(
        () => {
          navigate(next);
          setTravel(0);
          wake();
        },
        reduced() ? 0 : 180,
      );
    } else if (
      Math.hypot(event.clientX - previous.x, event.clientY - previous.y) < 8 &&
      phone()
    ) {
      const now = performance.now();
      if (now - lastTap < 300)
        setScale(zoom() > 1 ? 1 : 2, event.clientX, event.clientY);
      lastTap = now;
      wake();
    }
  };
  const frameStyle = () => {
    const r = rect();
    const drag = lightboxDragStyle(down());
    return {
      left: `${r.left}px`,
      top: `${r.top}px`,
      width: `${r.width}px`,
      height: `${r.height}px`,
      transform: `translate(${pan().x + travel()}px, ${pan().y + down()}px) scale(${zoom() * drag.scale})`,
      "border-radius": `${drag.radius}px`,
    };
  };
  return (
    <Portal>
      <div
        ref={dialog}
        class="lb-overlay"
        role="dialog"
        aria-modal="true"
        aria-labelledby={caption() ? "lb-caption" : undefined}
        aria-label={
          caption()
            ? undefined
            : `Image ${index() + 1} of ${props.images.length}`
        }
        data-idle={idle() ? "" : undefined}
        data-zoom={zoom() > 1 ? "" : undefined}
        data-closing={closing() ? "" : undefined}
        data-dragging={dragging() ? "" : undefined}
        onPointerDown={pointerDown}
        onPointerMove={pointerMove}
        onPointerUp={pointerEnd}
        onPointerCancel={pointerEnd}
        onWheel={(event) => {
          event.preventDefault();
          if (!phone() && native() > 1)
            setScale(
              zoom() * Math.exp(-event.deltaY * 0.002),
              event.clientX,
              event.clientY,
            );
        }}
      >
        <div
          ref={scrim}
          class="lb-scrim"
          style={{ opacity: lightboxDragStyle(down()).opacity / 0.94 }}
        />
        <Show when={phone() && zoom() === 1 && travel() !== 0}>
          <For each={[-1, 1]}>
            {(offset) => {
              const neighbour = () => props.images[index() + offset];
              return (
                <Show when={neighbour()}>
                  {(member) => {
                    const r = fittedRect(member(), innerWidth, innerHeight);
                    return (
                      <img
                        class="lb-neighbour"
                        src={member().element.currentSrc || member().src}
                        alt=""
                        style={{
                          left: `${r.left}px`,
                          top: `${r.top}px`,
                          width: `${r.width}px`,
                          height: `${r.height}px`,
                          transform: `translateX(${offset * (innerWidth + 24) + travel()}px)`,
                        }}
                      />
                    );
                  }}
                </Show>
              );
            }}
          </For>
        </Show>
        <div ref={frame} class="lb-frame" style={frameStyle()}>
          <Show
            when={!failed()}
            fallback={
              <div class="lb-failure">
                <Icon name="status-broken" size={24} />
                <span>Image could not be loaded.</span>
                <button type="button" onClick={openOriginal}>
                  Open original <Icon name="open-original" />
                </button>
              </div>
            }
          >
            <img
              class="lb-image"
              classList={{ "lb-soft": !!current().variants }}
              src={current().element.currentSrc || current().src}
              alt={current().alt}
              draggable={false}
            />
            <Show when={sharp()}>
              <img
                class="lb-image lb-sharp"
                src={sharp()}
                alt=""
                draggable={false}
              />
            </Show>
            <Show when={loading()}>
              <div class="lb-progress">
                <i />
              </div>
            </Show>
          </Show>
        </div>
        <Show when={!phone() && props.images.length > 1 && zoom() === 1}>
          <For each={[-1, 1]}>
            {(direction) => (
              <div
                class={`lb-zone lb-zone-${direction === -1 ? "prev" : "next"}`}
              >
                <button
                  type="button"
                  class="lb-control lb-nav"
                  aria-label={
                    direction === -1 ? "Previous image" : "Next image"
                  }
                  aria-disabled={
                    imageIndex(index(), direction, props.images.length) ===
                    index()
                  }
                  onClick={() => navigate(index() + direction)}
                >
                  <Icon
                    name={direction === -1 ? "previous-item" : "next-item"}
                    size={20}
                  />
                </button>
                <button
                  type="button"
                  class="lb-zone-hit"
                  tabindex="-1"
                  aria-label={
                    direction === -1 ? "Previous image zone" : "Next image zone"
                  }
                  onClick={() => navigate(index() + direction)}
                />
              </div>
            )}
          </For>
        </Show>
        <header class="lb-top lb-chrome">
          <div class="lb-identity">
            <Show when={props.images.length > 1}>
              <span class="lb-counter" aria-live="polite" aria-atomic="true">
                {shownIndex() + 1} / {props.images.length}
                <Show when={phone() && zoom() > 1}>
                  {" "}
                  · {zoom().toFixed(1)}×
                </Show>
              </span>
            </Show>
            <Show when={zoom() > 1 && (!phone() || props.images.length === 1)}>
              <span class="lb-ratio">
                {phone()
                  ? `${zoom().toFixed(1)}×`
                  : Math.abs(zoom() - native()) < 0.01
                    ? "1:1"
                    : `${zoom().toFixed(1)}×`}
              </span>
            </Show>
          </div>
          <div class="lb-actions">
            <Show when={native() > 1}>
              <button
                type="button"
                class="lb-control"
                aria-label={zoom() > 1 ? "Zoom out" : "Zoom in"}
                onClick={toggleZoom}
              >
                <Icon name={zoom() > 1 ? "zoom-out" : "zoom-in"} size={20} />
              </button>
            </Show>
            <button
              type="button"
              class="lb-control"
              aria-label="Open original image"
              onClick={openOriginal}
            >
              <Icon name="open-original" size={20} />
            </button>
            <Show when={props.onOpenReader}>
              <button
                type="button"
                class="lb-control"
                aria-label="Open in reader"
                onClick={openReader}
              >
                <Icon name="newspaper" size={20} />
              </button>
            </Show>
            <button
              ref={closeButton}
              type="button"
              class="lb-control lb-close"
              aria-label="Close lightbox"
              onClick={() => close()}
            >
              <Icon name="close" size={20} />
            </button>
          </div>
        </header>
        <Show when={caption() || previousCaption() || phone()}>
          <footer class="lb-caption lb-chrome">
            <Show when={caption()} keyed>
              {(text) => <p id="lb-caption">{text}</p>}
            </Show>
            <Show when={previousCaption()}>
              {(text) => (
                <p class="lb-caption-previous" aria-hidden="true">
                  {text()}
                </p>
              )}
            </Show>
            <Show when={phone()}>
              <i class="lb-grabber" />
            </Show>
          </footer>
        </Show>
        <Show
          when={phone() && props.images.length > 1 && props.images.length <= 8}
        >
          <div class="lb-dots" aria-hidden="true">
            <For each={props.images}>
              {(_, i) => (
                <span data-current={i() === shownIndex() ? "" : undefined} />
              )}
            </For>
          </div>
        </Show>
        <Show when={props.images.length >= 9}>
          <div class="lb-strip">
            <For each={props.images}>
              {(member, i) => {
                let tile!: HTMLButtonElement;
                createEffect(() => {
                  if (index() === i())
                    tile?.scrollIntoView({
                      block: "nearest",
                      inline: "nearest",
                    });
                });
                return (
                  <button
                    ref={tile}
                    type="button"
                    aria-label={`Image ${i() + 1}`}
                    aria-current={i() === index()}
                    onClick={() => navigate(i())}
                  >
                    <img src={member.element.currentSrc || member.src} alt="" />
                  </button>
                );
              }}
            </For>
          </div>
        </Show>
        <Show
          when={
            !phone() &&
            zoom() > 1 &&
            rect().width * zoom() > innerWidth &&
            rect().height * zoom() > innerHeight
          }
        >
          <div class="lb-minimap" style={{ opacity: mapVisible() ? 1 : 0 }}>
            <img src={current().element.currentSrc || current().src} alt="" />
            <i
              style={{
                width: `${(100 * innerWidth) / (rect().width * zoom())}%`,
                height: `${(100 * innerHeight) / (rect().height * zoom())}%`,
                left: `${50 - (100 * (innerWidth / 2 + pan().x)) / (rect().width * zoom())}%`,
                top: `${50 - (100 * (innerHeight / 2 + pan().y)) / (rect().height * zoom())}%`,
              }}
            />
          </div>
        </Show>
        <Show when={help()}>
          <section class="lb-help">
            <header>
              IMAGE · {index() + 1} OF {props.images.length}
              <button
                type="button"
                class="lb-control"
                aria-label="Close image keyboard help"
                onClick={() => setHelp(false)}
              >
                <Icon name="close" />
              </button>
            </header>
            <p>Reader keys are suspended while this is open.</p>
            <For
              each={
                props.onOpenReader
                  ? [...lightboxBindings, ["o", "Open in reader"]]
                  : lightboxBindings
              }
            >
              {([key, label]) => (
                <div>
                  <kbd>{key}</kbd>
                  <span>{label}</span>
                </div>
              )}
            </For>
          </section>
        </Show>
      </div>
    </Portal>
  );
}
