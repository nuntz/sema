import { expect, test } from "@playwright/test";

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
