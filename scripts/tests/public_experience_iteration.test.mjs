import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");

test("UX2 theme follows the system before an explicit light or dark choice", async () => {
  const [layout, toggle, css, header] = await Promise.all([
    read("apps/display-web/app/layout.tsx"),
    read("apps/display-web/components/theme-toggle.tsx"),
    read("apps/display-web/app/globals.css"),
    read("apps/display-web/components/site-header.tsx"),
  ]);
  assert.match(layout, /cookies\(\)/);
  assert.match(layout, /preference === "light" \|\| preference === "dark"/);
  assert.doesNotMatch(layout, /dangerouslySetInnerHTML|localStorage/);
  assert.match(toggle, /mujian_theme=\$\{next\}/);
  assert.match(toggle, /prefers-color-scheme: dark/);
  assert.match(header, /<ThemeToggle \/>/);
  assert.match(css, /html\[data-theme="dark"\]/);
  assert.match(css, /html:not\(\[data-theme="light"\]\)/);
  assert.match(css, /--surface:/);
  assert.match(css, /--header-bg:/);
});

test("UX2 theme keeps advertising and form surfaces on semantic colors", async () => {
  const css = await read("apps/display-web/app/globals.css");
  assert.match(css, /\.ad-slot \{[^}]*background: var\(--ad-surface\)/);
  assert.match(css, /\.work-card \{[^}]*background: var\(--surface\)/);
  assert.match(css, /\.auth-form input \{[^}]*background: var\(--surface\)/);
  assert.doesNotMatch(css, /background: white/);
});
