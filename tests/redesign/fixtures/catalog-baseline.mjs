const uuid = (prefix, index) => `${prefix}0000000-0000-4000-8000-${String(index).padStart(12, "0")}`;

const performer = (index, fields) => ({
  entity_type: "performer",
  id: uuid("2", index),
  slug: `rd-performer-${String(index).padStart(2, "0")}`,
  aliases: [],
  image_url: `https://media.example.test/redesign/performer-${index}.webp`,
  publication_status: "published",
  public_updated_at: "2026-09-16T06:00:00Z",
  ...fields,
});

const studio = (index, fields) => ({
  entity_type: "studio",
  id: uuid("3", index),
  slug: `rd-studio-${String(index).padStart(2, "0")}`,
  publication_status: "published",
  public_updated_at: "2026-09-16T06:00:00Z",
  ...fields,
});

const work = (index, fields) => ({
  entity_type: "work",
  id: uuid("1", index),
  slug: `rd-work-${String(index).padStart(2, "0")}`,
  code: `RD-${String(index).padStart(3, "0")}`,
  title: `重设计合成作品 ${String(index).padStart(2, "0")}`,
  title_original: null,
  release_date: `2026-08-${String(((index - 1) % 28) + 1).padStart(2, "0")}`,
  image_url: `https://media.example.test/redesign/work-${index}.webp`,
  studio_id: uuid("3", 1),
  performer_ids: [uuid("2", ((index - 1) % 3) + 1)],
  publication_status: "published",
  public_updated_at: "2026-09-16T06:00:00Z",
  content_classification: "non_explicit",
  image_preview_permission: "allow",
  edge_cases: [],
  ...fields,
});

export const baselineMetadata = Object.freeze({
  version: "rd-v1-dev-01",
  review_base_commit: "d79b304",
  schema_version_at_review: 21,
  generated_at: "2026-09-16T17:00:00+08:00",
  data_classification: "synthetic",
  contains_real_accounts_or_history: false,
});

export const performers = Object.freeze([
  performer(1, { name: "春野澪", name_original: "春野みお", aliases: ["小澪", "Mio Haruno"], edge_cases: ["alias", "multilingual"] }),
  performer(2, { name: "星野葵", aliases: ["Aoi Hoshino"], edge_cases: ["same_name"] }),
  performer(3, { name: "星野葵", slug: "rd-performer-hoshino-aoi-2", aliases: ["葵二号"], edge_cases: ["same_name"] }),
  performer(4, { name: "草稿人物", publication_status: "draft", public_updated_at: null, edge_cases: ["draft"] }),
  performer(5, { name: "隐藏人物", publication_status: "hidden", edge_cases: ["hidden"] }),
  performer(6, { name: "权利下架人物", publication_status: "takedown", edge_cases: ["takedown"] }),
  performer(7, { name: "水無月レイ", aliases: ["水无月玲", "Rei Minazuki"], image_url: null, edge_cases: ["no_image", "multilingual"] }),
]);

export const studios = Object.freeze([
  studio(1, { name: "青空制作", edge_cases: ["normal"] }),
  studio(2, { name: "青空制作", slug: "rd-studio-blue-sky-2", edge_cases: ["same_name"] }),
  studio(3, { name: "草稿厂牌", publication_status: "draft", public_updated_at: null, edge_cases: ["draft"] }),
  studio(4, { name: "隐藏厂牌", publication_status: "hidden", edge_cases: ["hidden"] }),
  studio(5, { name: "权利下架厂牌", publication_status: "takedown", edge_cases: ["takedown"] }),
]);

export const works = Object.freeze([
  work(1, {
    code: "LONG-001",
    title: "这是一个用于验证手机详情页、搜索结果卡片、结构化数据与面包屑不会截断关键信息的超长合成作品标题：春日海边摄影计划特别增补版（仅测试）",
    performer_ids: [uuid("2", 1), uuid("2", 7)],
    edge_cases: ["long_title", "multiple_performers"],
  }),
  work(2, { code: "NOIMG-002", image_url: null, image_preview_permission: "unknown", edge_cases: ["no_image"] }),
  work(3, { code: "NODATE-003", release_date: null, edge_cases: ["unknown_release_date"] }),
  work(4, { code: "SAME-004", title: "同番号版本 A", edge_cases: ["same_code", "version_a"] }),
  work(5, { code: "SAME-004", title: "同番号版本 B", release_date: "2026-08-05", edge_cases: ["same_code", "version_b"] }),
  work(6, { code: "JP-006", title: "夏の記録", title_original: "夏の記録 オリジナル", performer_ids: [uuid("2", 7)], edge_cases: ["multilingual"] }),
  work(7, { code: "ALIAS-007", title: "别名关联查询样本", performer_ids: [uuid("2", 1)], edge_cases: ["performer_alias_search"] }),
  work(8, { code: "DRAFT-008", title: "未发布草稿作品", publication_status: "draft", public_updated_at: null, edge_cases: ["draft"] }),
  work(9, { code: "HIDDEN-009", title: "管理员隐藏作品", publication_status: "hidden", edge_cases: ["hidden"] }),
  work(10, { code: "TAKE-010", title: "权利下架作品", publication_status: "takedown", edge_cases: ["takedown"] }),
  work(11, { code: "STUDIO-011", title: "同名厂牌版本一", studio_id: uuid("3", 1), edge_cases: ["same_studio_name"] }),
  work(12, { code: "STUDIO-012", title: "同名厂牌版本二", studio_id: uuid("3", 2), edge_cases: ["same_studio_name"] }),
  work(13, { code: "RATING-013", content_classification: "explicit", image_preview_permission: "deny", edge_cases: ["explicit", "image_denied"] }),
  work(14, { code: "UNKNOWN-014", content_classification: "unknown", image_preview_permission: "unknown", edge_cases: ["classification_unknown", "image_permission_unknown"] }),
  work(15, { code: "DATE-015", release_date: "2026-08-15", public_updated_at: "2026-09-15T23:59:59Z", edge_cases: ["timezone_boundary"] }),
  work(16, { code: "MULTI-016", performer_ids: [uuid("2", 2), uuid("2", 3)], edge_cases: ["same_name_performers"] }),
  work(17, { code: "NULLSTUDIO-017", studio_id: null, edge_cases: ["no_studio"] }),
  work(18, { code: "OLDAPI-018", public_updated_at: undefined, edge_cases: ["legacy_missing_public_updated_at"] }),
  work(19, { code: "PAG-019", release_date: "2026-01-01", edge_cases: ["pagination_tie"] }),
  work(20, { code: "PAG-020", release_date: "2026-01-01", edge_cases: ["pagination_tie"] }),
  work(21, { code: "SEARCH-021", title: "青空人物关联作品", performer_ids: [uuid("2", 1)], studio_id: uuid("3", 1), edge_cases: ["multi_field_search"] }),
  work(22, { code: "SEARCH-022", title: "水无月别名命中作品", performer_ids: [uuid("2", 7)], edge_cases: ["alias_normalization"] }),
  work(23, { code: "COPY-023", title: "番号复制降级测试", edge_cases: ["clipboard_fallback"] }),
  work(24, { code: "ERROR-024", title: "故障恢复后仍可辨认的作品", edge_cases: ["error_recovery"] }),
]);

export const coreCatalog = Object.freeze([...works, ...performers, ...studios]);

const expected = (query, type, expected_ids, purpose, excluded_ids = []) => ({
  query,
  type,
  expected_ids,
  excluded_ids,
  purpose,
});

export const labelledQueries = Object.freeze([
  expected("LONG-001", "work", [uuid("1", 1)], "exact_code"),
  expected("same 004", "work", [uuid("1", 4), uuid("1", 5)], "normalized_same_code_keeps_versions"),
  expected("春野澪", "work", [uuid("1", 1), uuid("1", 7), uuid("1", 21)], "performer_name_finds_related_works"),
  expected("小澪", "work", [uuid("1", 1), uuid("1", 7), uuid("1", 21)], "performer_alias_finds_related_works"),
  expected("青空制作", "work", [uuid("1", 11), uuid("1", 12), uuid("1", 21)], "same_name_studios_are_not_merged"),
  expected("夏の記録", "work", [uuid("1", 6)], "japanese_title"),
  expected("不存在的番号", "work", [], "no_result"),
  expected("HIDDEN-009", "work", [], "hidden_is_excluded", [uuid("1", 9)]),
  expected("TAKE-010", "work", [], "takedown_is_excluded", [uuid("1", 10)]),
  expected("DRAFT-008", "work", [], "draft_is_excluded", [uuid("1", 8)]),
  expected("水无月玲", "work", [uuid("1", 6), uuid("1", 22)], "localized_alias"),
  expected("故障恢复", "work", [uuid("1", 24)], "query_preserved_after_retry"),
  expected("春野澪", "performer", [uuid("2", 1)], "canonical_name"),
  expected("Mio Haruno", "performer", [uuid("2", 1)], "romanized_alias"),
  expected("星野葵", "performer", [uuid("2", 2), uuid("2", 3)], "same_name_entities_not_merged"),
  expected("水無月レイ", "performer", [uuid("2", 7)], "japanese_name"),
  expected("Rei Minazuki", "performer", [uuid("2", 7)], "latin_alias"),
  expected("隐藏人物", "performer", [], "hidden_performer_excluded", [uuid("2", 5)]),
  expected("权利下架人物", "performer", [], "takedown_performer_excluded", [uuid("2", 6)]),
  expected("青空制作", "studio", [uuid("3", 1), uuid("3", 2)], "same_name_studios_not_merged"),
  expected("草稿厂牌", "studio", [], "draft_studio_excluded", [uuid("3", 3)]),
  expected("隐藏厂牌", "studio", [], "hidden_studio_excluded", [uuid("3", 4)]),
  expected("权利下架厂牌", "studio", [], "takedown_studio_excluded", [uuid("3", 5)]),
  expected("没有这个厂牌", "studio", [], "studio_no_result"),
]);

export const boundaryRequirements = Object.freeze({
  performer_association_count: 101,
  studio_association_count: 201,
  sitemap_entry_count: 50_001,
  sitemap_max_urls_per_file: 50_000,
});

export function makeAssociationWorks({ parentType, count }) {
  if (!["performer", "studio"].includes(parentType)) throw new TypeError("parentType must be performer or studio");
  if (!Number.isInteger(count) || count < 1) throw new TypeError("count must be a positive integer");
  const parentId = parentType === "performer" ? uuid("2", 900) : uuid("3", 900);
  return Array.from({ length: count }, (_, offset) => {
    const index = offset + 1;
    return work(10_000 + index, {
      slug: `rd-${parentType}-association-${String(index).padStart(4, "0")}`,
      code: `${parentType === "performer" ? "PERF" : "STUD"}-${String(index).padStart(4, "0")}`,
      title: `${parentType === "performer" ? "人物" : "厂牌"}关联分页样本 ${index}`,
      release_date: index % 11 === 0 ? null : `2025-${String(((index - 1) % 12) + 1).padStart(2, "0")}-${String(((index - 1) % 28) + 1).padStart(2, "0")}`,
      performer_ids: parentType === "performer" ? [parentId] : [uuid("2", 1)],
      studio_id: parentType === "studio" ? parentId : uuid("3", 1),
      edge_cases: ["association_pagination"],
    });
  });
}

export function* makeSitemapEntries(count = boundaryRequirements.sitemap_entry_count) {
  if (!Number.isInteger(count) || count < 1) throw new TypeError("count must be a positive integer");
  for (let index = 1; index <= count; index += 1) {
    yield {
      entity_type: index % 3 === 0 ? "performer" : index % 5 === 0 ? "studio" : "work",
      slug: `rd-sitemap-${String(index).padStart(6, "0")}`,
      lastmod: `2026-09-${String(((index - 1) % 16) + 1).padStart(2, "0")}`,
      indexable: true,
    };
  }
}
