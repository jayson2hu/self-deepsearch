import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  checkMarkdownLinks,
  collectMarkdownFiles,
  extractMarkdownTargets,
} from "../check_markdown_links.mjs";

function withRepository(run) {
  const root = mkdtempSync(path.join(os.tmpdir(), "markdown-links-"));
  mkdirSync(path.join(root, "docs", "nested"), { recursive: true });
  try {
    run(root);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

test("collects only repository-root and docs Markdown files", () => {
  withRepository((root) => {
    writeFileSync(path.join(root, "README.md"), "root");
    writeFileSync(path.join(root, "docs", "guide.md"), "guide");
    writeFileSync(path.join(root, "docs", "nested", "evidence.md"), "evidence");
    mkdirSync(path.join(root, "vendor"));
    writeFileSync(path.join(root, "vendor", "ignored.md"), "ignored");
    assert.deepEqual(
      collectMarkdownFiles(root).map((file) => path.relative(root, file).replaceAll("\\", "/")),
      ["docs/guide.md", "docs/nested/evidence.md", "README.md"],
    );
  });
});

test("extracts normal, image, and angle-bracket Markdown targets", () => {
  assert.deepEqual(
    extractMarkdownTargets("[guide](./guide.md) ![shot](./shot.png) [space](<./has space.md>)"),
    ["./guide.md", "./shot.png", "./has space.md"],
  );
});

test("accepts existing local links with fragments, queries, and percent encoding", () => {
  withRepository((root) => {
    writeFileSync(path.join(root, "README.md"), "[guide](./docs/guide.md#part) [asset](./docs/has%20space.txt?v=1)");
    writeFileSync(path.join(root, "docs", "guide.md"), "[root](/README.md)");
    writeFileSync(path.join(root, "docs", "has space.txt"), "asset");
    const report = checkMarkdownLinks(root);
    assert.equal(report.status, "passed");
    assert.equal(report.markdown_files, 2);
    assert.equal(report.local_links, 3);
    assert.deepEqual(report.missing, []);
  });
});

test("ignores external protocols, protocol-relative URLs, and page fragments", () => {
  withRepository((root) => {
    writeFileSync(
      path.join(root, "README.md"),
      "[web](https://example.test) [mail](mailto:rights@example.test) [cdn](//cdn.example.test/a.png) [part](#part)",
    );
    const report = checkMarkdownLinks(root);
    assert.equal(report.status, "passed");
    assert.equal(report.local_links, 0);
  });
});

test("reports missing, invalid, and outside-repository local targets", () => {
  withRepository((root) => {
    writeFileSync(path.join(root, "README.md"), "[missing](./none.md) [bad](./bad%ZZ.md) [outside](../outside.md)");
    const report = checkMarkdownLinks(root);
    assert.equal(report.status, "failed");
    assert.equal(report.local_links, 3);
    assert.deepEqual(report.missing.map((item) => item.reason), ["missing", "invalid_percent_encoding", "outside_repository"]);
  });
});
