"use client";

import type { MediaManifestRequest, PrimaryMediaStateResponse } from "@self-deepsearch/api-contracts";
import { FileJson, Save, ShieldCheck, X } from "lucide-react";
import { ChangeEvent, FormEvent, useRef, useState } from "react";
import { getPrimaryMedia, publishMedia } from "../lib/media-client";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const SHA256 = /^[a-f0-9]{64}$/;
const RENDITIONS = new Set(["master", "w320", "w640", "w960"]);

function readManifest(value: unknown): MediaManifestRequest {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("清单必须是 JSON 对象");
  const item = value as Partial<MediaManifestRequest>;
  if (!item.asset_id || !UUID.test(item.asset_id) || !item.entity_id || !UUID.test(item.entity_id)) throw new Error("资产或资料 ID 不符合要求");
  if (item.tool_version !== "media-python/1") throw new Error("清单不是由当前媒体处理器生成");
  if (item.entity_type !== "work" && item.entity_type !== "performer") throw new Error("资料类型不符合要求");
  const validDisplaySlot = Number.isInteger(item.position) && (
    item.entity_type === "work" && item.purpose === "cover" && item.is_primary === true && item.position === 0
    || item.entity_type === "work" && item.purpose === "gallery" && item.is_primary === false && item.position! >= 1 && item.position! <= 3
    || item.entity_type === "performer" && item.purpose === "avatar" && item.is_primary === true && item.position === 0
  );
  if (!validDisplaySlot) throw new Error("主图、用途和展示位置不符合 1 张主图 + 最多 3 张精选图的规则");
  if (!Array.isArray(item.objects) || item.objects.length !== 4) throw new Error("清单必须包含母版和三档派生图");
  const renditions = new Set<string>();
  for (const object of item.objects) {
    if (!object || !RENDITIONS.has(object.rendition) || renditions.has(object.rendition)) throw new Error("图片尺寸版本缺失或重复");
    if (!object.storage_key || !object.backup_path || !SHA256.test(object.sha256) || object.byte_size < 1 || object.width < 1 || object.height < 1) throw new Error("图片对象元数据不完整");
    if ((object.rendition === "master" && (object.storage_scope !== "private" || !object.storage_key.startsWith("media-master/"))) || (object.rendition !== "master" && (object.storage_scope !== "public" || !object.storage_key.startsWith("media-public/")))) throw new Error("存储范围与图片版本不匹配");
    if (object.backup_path !== object.storage_key || (object.public_url && !new URL(object.public_url).pathname.endsWith(`/${object.storage_key}`))) throw new Error("S3、北京副本和公开地址没有一一对应");
    if (object.rendition === "master" ? object.public_url !== null : !object.public_url?.startsWith("https://")) throw new Error("母版必须私有，派生图必须使用 HTTPS 地址");
    renditions.add(object.rendition);
  }
  if ([...RENDITIONS].some((rendition) => !renditions.has(rendition))) throw new Error("清单缺少必要尺寸");
  return item as MediaManifestRequest;
}

export function MediaManifestForm() {
  const sequence = useRef(0);
  const fileInput = useRef<HTMLInputElement>(null);
  const mutating = useRef(false);
  const [manifest, setManifest] = useState<MediaManifestRequest | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [created, setCreated] = useState<Awaited<ReturnType<typeof publishMedia>> | null>(null);
  const [primary, setPrimary] = useState<PrimaryMediaStateResponse | null>(null);
  const [lookup, setLookup] = useState<"idle" | "loading" | "ready" | "error">("idle");
  const [replaceConfirmed, setReplaceConfirmed] = useState(false);
  const oldAsset = primary?.primary?.asset_id ?? null;
  const alreadyCurrent = !!manifest && oldAsset?.toLowerCase() === manifest.asset_id.toLowerCase();
  const parentAllowed = !!primary && !["takedown", "merged"].includes(primary.entity_status);
  const primaryReady = !manifest?.is_primary || (lookup === "ready" && parentAllowed && !alreadyCurrent && (!oldAsset || replaceConfirmed));

  async function readPrimary(item: MediaManifestRequest, version: number) {
    setPrimary(null); setLookup("loading"); setReplaceConfirmed(false); setConfirmed(false);
    try {
      const state = await getPrimaryMedia(item);
      if (sequence.current !== version) return;
      setPrimary(state); setLookup("ready");
    } catch (error) {
      if (sequence.current !== version) return;
      setLookup("error"); setMessage(error instanceof Error ? error.message : "无法读取当前主图");
    }
  }

  async function load(event: ChangeEvent<HTMLInputElement>) {
    if (mutating.current) return;
    const version = ++sequence.current;
    setManifest(null); setConfirmed(false); setCreated(null); setMessage("");
    setPrimary(null); setLookup("idle"); setReplaceConfirmed(false);
    const file = event.target.files?.[0];
    if (!file) return;
    if (file.size > 64 * 1024) { setMessage("manifest 文件不能超过 64 KiB"); return; }
    try {
      const item = readManifest(JSON.parse(await file.text()));
      if (sequence.current !== version) return;
      setManifest(item);
      if (item.is_primary) await readPrimary(item, version);
    } catch (error) {
      if (sequence.current !== version) return;
      setMessage(error instanceof Error ? error.message : "无法读取 manifest 文件");
    }
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (mutating.current || !manifest || !confirmed || !primaryReady) return;
    mutating.current = true;
    setBusy(true); setMessage(""); setCreated(null);
    try {
      const result = await publishMedia(manifest, manifest.is_primary ? oldAsset : null);
      setCreated(result);
      ++sequence.current;
      setManifest(null); setConfirmed(false); setPrimary(null); setLookup("idle"); setReplaceConfirmed(false);
      if (fileInput.current) fileInput.current.value = "";
    } catch (error) {
      setMessage(`${error instanceof Error ? error.message : "无法连接运营服务，提交结果未确认"} 清单仍保留。`);
      setConfirmed(false); setReplaceConfirmed(false);
      if (manifest.is_primary) { setPrimary(null); setLookup("error"); }
    } finally {
      mutating.current = false;
      setBusy(false);
    }
  }

  return <form className="entity-form" onSubmit={submit}>
    <label className="manifest-upload"><span>处理器生成的 manifest</span><input accept=".json,application/json" disabled={busy} onChange={load} ref={fileInput} type="file" /></label>
    {manifest ? <section className="manifest-preview">
      <div className="manifest-heading"><div><h2><FileJson size={17} />清单核对</h2><p>元数据只读；需要修改时请重新运行媒体处理器。</p></div><button aria-label="移除清单" className="icon-button" disabled={busy} onClick={() => { ++sequence.current; setManifest(null); setConfirmed(false); setPrimary(null); setLookup("idle"); setMessage(""); if (fileInput.current) fileInput.current.value = ""; }} title="移除" type="button"><X size={17} /></button></div>
      <dl className="manifest-summary">
        <div><dt>资料</dt><dd>{manifest.entity_type} / <code>{manifest.entity_id}</code></dd></div>
        <div><dt>资产</dt><dd><code>{manifest.asset_id}</code></dd></div>
        <div><dt>来源</dt><dd>{manifest.source_type}{manifest.source_url ? ` / ${manifest.source_url}` : ""}</dd></div>
        <div><dt>用途</dt><dd>{manifest.purpose} / {manifest.is_primary ? "主图" : `位置 ${manifest.position}`}</dd></div>
      </dl>
      <div aria-label="图片对象清单，可横向滚动" className="table-wrap manifest-objects" role="region" tabIndex={0}><table><thead><tr><th>版本</th><th>存储</th><th>尺寸</th><th>大小</th><th>哈希</th><th>公开</th></tr></thead><tbody>{manifest.objects.map((object) => <tr key={object.rendition}><td>{object.rendition}</td><td>{object.storage_scope}</td><td>{object.width} x {object.height}</td><td>{Math.ceil(object.byte_size / 1024)} KiB</td><td><code>{object.sha256.slice(0, 12)}...</code></td><td>{object.public_url ? "是" : "否"}</td></tr>)}</tbody></table></div>
      {manifest.is_primary ? <section aria-label="主图替换确认" className="manifest-replacement">
        <h3>当前主图核对</h3>
        {lookup === "loading" ? <p role="status">正在读取当前主图…</p> : null}
        {lookup === "error" ? <p>当前主图未确认，提交已暂停。请刷新核对后重试。</p> : null}
        {primary ? <>
          <p>资料状态：{primary.entity_status}；{oldAsset ? <>当前资产：<code>{oldAsset}</code></> : "当前没有主图，将登记为第一张主图。"}</p>
          {!parentAllowed ? <p className="form-error">资料已下架或合并，不能发布图片。</p> : null}
          {alreadyCurrent ? <p className="form-error">该资产已是当前主图，无需重复提交。若上次响应丢失，请核对审计记录。</p> : null}
          {oldAsset && !alreadyCurrent && parentAllowed ? <>
            <p>将切换为上方的新资产和新地址。旧资产无其他公开关联时，公开图立即安排删除，私有母版保留 30 天后安排删除；权利下架可提前结束保留。若仍有其他关联，旧资产继续保留。</p>
            <p>切换与任务排队同事务完成；缓存刷新、S3 和北京副本的实际删除由后台异步执行。</p>
            <label className="check-label"><input checked={replaceConfirmed} disabled={busy} onChange={(event) => setReplaceConfirmed(event.target.checked)} type="checkbox" /><span>确认替换以上旧主图，并了解旧图保留与删除规则</span></label>
          </> : null}
        </> : null}
        <button disabled={busy || lookup === "loading"} onClick={() => { if (manifest) { setMessage(""); void readPrimary(manifest, ++sequence.current); } }} type="button">刷新当前主图</button>
      </section> : null}
      <label className="check-label manifest-confirm"><input checked={confirmed} disabled={busy} onChange={(event) => setConfirmed(event.target.checked)} type="checkbox" /><span><ShieldCheck size={16} />已核对来源、展示权利、实体关联和北京副本</span></label>
    </section> : <p className="empty-copy">先在北京媒体处理器完成安全解码、S3 上传和备份，再导入生成的 JSON 清单。</p>}
    {message ? <p aria-live="polite" className="form-error">{message}</p> : null}
    {created ? <div aria-live="polite" className="form-success"><p>{created.replacement ? "主图已切换" : "图片资产已发布"}：<code>{created.media.asset_id}</code>，登记 {created.media.object_count} 个对象。</p>{created.replacement ? <p>{created.replacement.old_asset_retired ? `旧公开图删除已排队；私有母版保留至 ${created.replacement.private_retained_until}。实际删除尚未确认。` : "旧图仍有其他公开关联，旧资产及其对象已保留，未安排删除。"}</p> : null}<p>公开展示仍以资料发布状态为准，缓存刷新异步执行。</p></div> : null}
    <div className="form-actions"><button className="primary" disabled={busy || !manifest || !confirmed || !primaryReady} type="submit"><Save size={17} />{busy ? "提交中" : manifest?.is_primary && oldAsset ? "确认替换主图" : "核对并发布图片"}</button></div>
  </form>;
}
