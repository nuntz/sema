import { expect, test } from "@playwright/test";

test("grid image explicitly decodes after its deferred source loads", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const decode = HTMLImageElement.prototype.decode;
    HTMLImageElement.prototype.decode = function () {
      this.setAttribute("data-decode-source", this.currentSrc);
      return decode.call(this);
    };
  });
  await page.goto("/e2e/image-loading-fixture.html");
  const image = page.getByAltText("Boundary image");
  await expect(image).not.toHaveAttribute("src");
  await expect(image).not.toHaveAttribute("data-decode-source");

  await image.evaluate((element) => {
    element.style.top = "0px";
  });
  await expect(image).toHaveAttribute(
    "data-decode-source",
    /\/sema-mark\.svg$/,
  );
});

test("grid image loads when it touches the visibility margin", async ({
  page,
}) => {
  await page.goto("/e2e/image-loading-fixture.html");
  const image = page.getByAltText("Boundary image");
  // Wait for the browser's initial intersection delivery outside the margin.
  await image.evaluate(
    (element) =>
      new Promise<void>((resolve) => {
        const observer = new IntersectionObserver(
          () => {
            observer.disconnect();
            resolve();
          },
          {
            root: element.closest(".grid-scroll"),
            rootMargin: "100px 0px",
          },
        );
        observer.observe(element);
      }),
  );
  await expect(image).not.toHaveAttribute("src");
  await image.evaluate((element) => {
    element.style.top = "500px";
  });
  await expect(image).toHaveAttribute("src", "/sema-mark.svg");
  await expect
    .poll(() =>
      image.evaluate((element) => (element as HTMLImageElement).naturalWidth),
    )
    .toBeGreaterThan(0);
});
