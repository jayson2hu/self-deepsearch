import type { WorkCSVPreflightResponse } from "@self-deepsearch/api-contracts";

export function csvCell(value: string): string;
export function workImportErrorCSV(report: WorkCSVPreflightResponse): string;
