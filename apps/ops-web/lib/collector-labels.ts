export function candidateStatusLabel(status: string) {
  return ({ pending: "待审核", valid: "校验通过", invalid: "校验未通过", duplicate: "重复", conflict: "存在冲突", reviewing: "审核中", accepted: "已接受", rejected: "已拒绝" } as Record<string, string>)[status] ?? status;
}
