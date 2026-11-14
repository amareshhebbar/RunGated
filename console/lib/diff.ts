export interface Part {
  kind: "same" | "add" | "del";
  text: string;
}

export function wordDiff(a: string, b: string): Part[] {
  const x = a.split(/(\s+|[.,;:!?])/).filter((s) => s !== "");
  const y = b.split(/(\s+|[.,;:!?])/).filter((s) => s !== "");
  const n = x.length;
  const m = y.length;
  if (n * m > 4_000_000) {
    return [
      { kind: "del", text: a },
      { kind: "add", text: b },
    ];
  }
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = x[i] === y[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const out: Part[] = [];
  const push = (kind: Part["kind"], text: string) => {
    const last = out[out.length - 1];
    if (last && last.kind === kind) last.text += text;
    else out.push({ kind, text });
  };
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (x[i] === y[j]) {
      push("same", x[i]);
      i++;
      j++;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      push("del", x[i++]);
    } else {
      push("add", y[j++]);
    }
  }
  while (i < n) push("del", x[i++]);
  while (j < m) push("add", y[j++]);
  return out;
}
