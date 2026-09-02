export type ImageDecodeResult = "decoded" | "error" | "timeout";

export interface DecodableImage {
  decode?(): Promise<void>;
}

export function decodeImageWithin(
  image: DecodableImage,
  timeoutMS: number,
): Promise<ImageDecodeResult> {
  if (typeof image.decode !== "function") return Promise.resolve("decoded");

  return new Promise((resolve) => {
    let settled = false;
    const finish = (result: ImageDecodeResult) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(result);
    };
    const timer = setTimeout(() => finish("timeout"), timeoutMS);
    Promise.resolve()
      .then(() => image.decode?.())
      .then(() => finish("decoded"))
      .catch(() => finish("error"));
  });
}
