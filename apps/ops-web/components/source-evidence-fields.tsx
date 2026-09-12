"use client";

export function SourceEvidenceFields() {
  return <>
    <label><span>来源类型</span><select defaultValue="web_page" name="source_type" required>
      <option value="web_page">普通网页</option>
      <option value="search_result">搜索结果</option>
      <option value="official">官方资料</option>
      <option value="other">其他公开资料</option>
    </select></label>
    <label><span>核验时间</span><input defaultValue={localDateTimeValue(new Date())} name="source_checked_at" required step={1} suppressHydrationWarning type="datetime-local" /></label>
    <label className="wide"><span>来源 URL（可选）</span><input maxLength={2000} name="source_url" type="url" /></label>
    <label className="wide"><span>来源标题（可选）</span><input maxLength={300} name="source_title" /></label>
  </>;
}

export function sourceEvidenceFromForm(data: FormData) {
  const optional = (key: string) => String(data.get(key) ?? "").trim() || null;
  const checkedAt = String(data.get("source_checked_at") ?? "").trim();
  return [{
    source_type: String(data.get("source_type") ?? "web_page"),
    source_url: optional("source_url"),
    source_title: optional("source_title"),
    checked_at: new Date(checkedAt).toISOString(),
  }];
}

function localDateTimeValue(value: Date) {
  const offset = value.getTimezoneOffset() * 60_000;
  return new Date(value.getTime() - offset).toISOString().slice(0, 19);
}
