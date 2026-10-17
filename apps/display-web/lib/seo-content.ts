type WorkMetadataSource = {
  code: string;
  title: string;
  release_date?: string | null;
  studio_name?: string | null;
  studio?: { name: string } | null;
  performers: ReadonlyArray<{ name: string }>;
};

// Use only facts already visible on the page, not inferred translations,
// download keywords, ratings or unverified biographical claims.
export function workMetadataDescription(work: WorkMetadataSource): string {
  const facts = [
    `${work.code} ${work.title}`,
    work.release_date ? `发行日期：${work.release_date}` : "",
    work.studio?.name || work.studio_name ? `厂牌：${work.studio?.name || work.studio_name}` : "",
    work.performers.length ? `人物：${work.performers.slice(0, 3).map((person) => person.name).join("、")}` : "",
    "仅提供公开资料索引",
  ];
  const text = facts.filter(Boolean).join("。").replace(/[\u0000-\u001f\u007f\s]+/g, " ").trim();
  const characters = Array.from(text);
  return characters.length > 180 ? characters.slice(0, 179).join("") + "…" : text;
}
