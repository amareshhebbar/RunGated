#!/usr/bin/env bash
set -euo pipefail

NEW="${1:?usage: scripts/bump-version.sh X.Y.Z}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

case "$NEW" in
  [0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "version must look like 1.2.3"; exit 1 ;;
esac

OLD="$(sed -n 's/^var version = "\(.*\)"/\1/p' cmd/rungated/main.go)"
[ -n "$OLD" ] || { echo "could not read the current version"; exit 1; }
OLDRE="${OLD//./\.}"
[ "$OLD" != "$NEW" ] || { echo "already at $NEW"; exit 0; }

sed -i "s/^var version = \"$OLDRE\"/var version = \"$NEW\"/" cmd/rungated/main.go
sed -i "s/^__version__ = \"$OLDRE\"/__version__ = \"$NEW\"/" sdk/python/rungated/__init__.py
sed -i "s/^version = \"$OLDRE\"/version = \"$NEW\"/" sdk/python/pyproject.toml
sed -i "s/^export const version = \"$OLDRE\"/export const version = \"$NEW\"/" sdk/typescript/src/index.ts
sed -i "0,/\"version\": \"$OLDRE\"/s//\"version\": \"$NEW\"/" sdk/typescript/package.json
python3 - "$OLD" "$NEW" <<'PY'
import re, sys
old, new = sys.argv[1:3]
lock = "sdk/typescript/package-lock.json"
s = open(lock).read()
s = s.replace('"version": "%s"' % old, '"version": "%s"' % new, 2)
open(lock, "w").write(s)
PY
sed -i "s/^version: $OLDRE/version: $NEW/; s/^appVersion: \"$OLDRE\"/appVersion: \"$NEW\"/" deploy/helm/rungated/Chart.yaml
sed -i "s/\"Version\": \"$OLDRE\", \"AppVersion\": \"$OLDRE\"/\"Version\": \"$NEW\", \"AppVersion\": \"$NEW\"/; s#rungated:$OLDRE#rungated:$NEW#g" deploy/helm/chart_test.go
[ -f ] && sed -i "s/VERSION:-$OLDRE/VERSION:-$NEW/"

for f in docs/self_hosting/kubernetes.md docs/enterprise/airgap.md; do
  [ -f "$f" ] && sed -i "s/$OLDRE/$NEW/g" "$f"
done

echo "version $OLD -> $NEW"
grep -rnF "$OLD" cmd sdk/python/rungated sdk/python/pyproject.toml sdk/typescript/src sdk/typescript/package.json deploy docs --include=* -I 2>/dev/null | grep -v "\.venv\|node_modules\|/dist/" || echo "no leftovers"
