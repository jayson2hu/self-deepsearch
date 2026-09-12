const spreadsheetFormulaPrefix = /^[\u0000-\u0020]*[=+\-@]/;

export function csvCell(value) {
  const safe = spreadsheetFormulaPrefix.test(value) ? `'${value}` : value;
  return `"${safe.replaceAll('"', '""')}"`;
}

export function workImportErrorCSV(report) {
  const lines = [
    ["row_number", "code", "field", "error_code", "message"],
    ...report.file_issues.map((issue) => ["", "", issue.field, issue.code, issue.message]),
    ...report.rows.flatMap((row) => row.issues.map((issue) => [
      String(row.row_number),
      row.code,
      issue.field,
      issue.code,
      issue.message,
    ])),
  ];
  return `\ufeff${lines.map((line) => line.map(csvCell).join(",")).join("\r\n")}`;
}
