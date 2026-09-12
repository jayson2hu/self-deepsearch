import assert from "node:assert/strict";
import test from "node:test";

import { csvCell, workImportErrorCSV } from "../../apps/ops-web/lib/work-csv-errors.mjs";

test("CSV error cells neutralize spreadsheet formula prefixes", () => {
  for (const value of ["=HYPERLINK(\"https://invalid.test\")", "+SUM(1,1)", "-2+3", "@SUM(1,1)", "\t=1+1", "  =1+1"]) {
    assert.equal(csvCell(value), `"'${value.replaceAll('"', '""')}"`);
  }
  assert.equal(csvCell("ABC-123"), '"ABC-123"');
});

test("CSV error export is BOM-prefixed, quoted, and contains only validation rows", () => {
  const csv = workImportErrorCSV({
    total_count: 2,
    valid_count: 1,
    invalid_count: 1,
    file_issues: [{ field: "header", code: "UNKNOWN_HEADER", message: "未知,列" }],
    rows: [
      { row_number: 2, code: "SAFE-001", title: "valid", issues: [] },
      {
        row_number: 3,
        code: "=HYPERLINK(\"https://invalid.test\")",
        title: "invalid",
        issues: [{ field: "code", code: "INVALID_CODE", message: "番号\"错误" }],
      },
    ],
  });

  assert.ok(csv.startsWith('\ufeff"row_number","code","field","error_code","message"\r\n'));
  assert.ok(csv.includes('"","","header","UNKNOWN_HEADER","未知,列"'));
  assert.ok(csv.includes('"3","\'=HYPERLINK(""https://invalid.test"")","code","INVALID_CODE","番号""错误"'));
  assert.ok(!csv.includes("SAFE-001"));
});
