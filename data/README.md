# MVP 种子数据模板

这些 CSV 只定义人工录入和首轮导入字段，不包含真实人物、作品或图片。测试网站首批准备 30 个人物、100 部作品；真实数据不需要一次准备完整。

自动采集脚本、任务参数、候选 JSONL 和批次提交规则见上级目录 [COLLECTION_PLAN.md](../COLLECTION_PLAN.md)。

数据库仍以迁移文件和 API 合同为准，CSV 不是数据库 Schema。所有文件使用 UTF-8，首行为字段名。

## 填写顺序

1. `sources.csv`：登记来源类型、可选 URL、核验时间、操作人和权利状态。
2. `studios.csv`、`tags.csv`：建立厂牌和受控标签。
3. `performers.csv`、`aliases.csv`：建立人物、作品别名和多语言写法。
4. `works.csv`、`work_performers.csv`：建立作品、实际发行日期和人物关系。
5. `media_manifest.csv`、`rights_records.csv`：登记逻辑图片、S3/北京/公开 URL 映射和权利状态。
6. `ad_test_campaigns.csv`：只用于无第三方脚本的广告位视觉测试。

## 通用格式

- 内部 ID 使用 UUID；样例阶段可以由导入器生成，但关联文件必须使用同一 ID。
- `release_date` 使用 `YYYY-MM-DD`。
- `checked_at`、`verified_at` 等事件时间使用带时区 RFC 3339，例如 `2026-07-31T14:30:00+08:00`。
- 多值字段使用竖线 `|` 分隔，不在一个单元格中嵌套 JSON。
- 状态值必须来自文档定义的受控枚举，不允许自由填写。
- 图片只填写本地路径、S3 key 或 URL，不把二进制写入 CSV。
- 来源 URL 可以为空，但 `source_type`、`checked_at`、`operator_id` 和 `rights_status` 不可空。

## 作品编号

`canonical_code` 是标准化后的番号，用于搜索和判重，但**不是全局唯一键**。不同厂牌、重发版或特殊版本可以复用相同番号。

导入器使用以下信息生成重复候选：

```text
canonical_code
studio_id
release_date
performer_ids
source_ids
```

疑似重复必须进入人工审核，不由 CSV 导入直接覆盖已发布数据。重发或不同版本使用独立 `work_id`，通过 `edition_of` 关联。

## 图片清单

一个逻辑 `asset_id` 可以对应多个版本和派生对象：

```text
S3 私有母版
S3 320/640/960 公开派生图
北京备份路径
网站 public_url
Cloudflare 缓存 URL
```

同一 `asset_id + version + rendition` 只能有一条有效记录。没有图片时不创建伪造资产，前台使用人物或作品默认图。

## 导入状态

- 未完成来源或权利核验的记录只能进入 `draft` 或 `reviewing`；
- CSV 导入只写 `collector` Schema；
- 导入不能直接写 `published`；
- 管理员审核后由 Go 发布服务生成 revision 和 publication；
- 同一导入批次必须携带 `batch_id` 和幂等键，重复执行不能产生重复实体。
