export type Path = (string | number)[];

export function collectStrings(value: unknown, path: Path = []): { path: Path; text: string }[] {
  if (typeof value === "string") return [{ path, text: value }];
  if (Array.isArray(value)) return value.flatMap((v, i) => collectStrings(v, [...path, i]));
  if (value && typeof value === "object") {
    return Object.entries(value as Record<string, unknown>).flatMap(([k, v]) => collectStrings(v, [...path, k]));
  }
  return [];
}

export function replaceStrings<T>(value: T, strings: { path: Path; text: string }[], replacements: string[]): T {
  if (strings.length === 0) return value;
  if (strings[0].path.length === 0) return replacements[0] as unknown as T;
  const root = structuredClone(value) as any;
  strings.forEach((s, i) => {
    let cur = root;
    for (let j = 0; j < s.path.length - 1; j++) cur = cur[s.path[j]];
    cur[s.path[s.path.length - 1]] = replacements[i];
  });
  return root as T;
}
