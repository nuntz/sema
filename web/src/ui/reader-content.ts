const LEADING_IMAGE = /^\s*(?:(?:<p|<a|<figure)(?:\s[^>]*)?>\s*)*<img(?:\s|>)/i;

export function hasLeadingImage(markup: string): boolean {
  return LEADING_IMAGE.test(markup);
}
