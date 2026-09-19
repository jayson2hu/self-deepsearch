import assert from 'node:assert/strict';
import {readFileSync, writeFileSync, mkdirSync} from 'node:fs';
import path from 'node:path';
import {chromium, expect, request} from '@playwright/test';
import {streamedRenderFailureSignals} from './release_a_frontend_smoke.mjs';

const preview='http://127.0.0.1:13004', ops='http://127.0.0.1:13001', display='http://127.0.0.1:13000';
const out=path.resolve('docs/evidence/collector-integration-2026-09-19'); mkdirSync(out,{recursive:true});
const archive=path.resolve('../self-deepsearch-collector/runtime/jable-archive/20260919-final');
const records=readFileSync(path.join(archive,'selfdeepsearch-export/performers.jsonl'),'utf8').trim().split('\n').map(JSON.parse);
const media=readFileSync(path.join(archive,'performer_media_candidates.jsonl'),'utf8').trim().split('\n').map(JSON.parse);
const withAvatars=new Set(media.map(m=>m.performer_id));
assert.ok(process.env.INTEGRATION_OWNER_EMAIL && process.env.INTEGRATION_OWNER_PASSWORD,'owner credentials required via environment');
const report={status:'running',checked_at:new Date().toISOString(),checks:{},failures:[]};
const browser=await chromium.launch({channel:'chrome',headless:true});
const external=[], errors=[];
let stage='startup';
function passed(name,detail=true){report.checks[name]=detail;console.log(`PASS ${name}`);}
try {
  const anonymous=await request.newContext();
  for(const route of ['/','/api/performers',`/media/${media[0].media_candidate_id}.jpg`,'/preview.js']) {
    const response=await anonymous.get(preview+route);assert.equal(response.status(),401);
  }
  const forged=await anonymous.get(preview+'/api/performers',{headers:{Cookie:'sd_session=forged'}});assert.equal(forged.status(),401);
  passed('anonymous_and_forged_cookie_denied');
  assert.equal((await anonymous.get(preview+'/healthz')).status(),200);
  assert.match(await (await anonymous.get(preview+'/robots.txt')).text(),/Disallow: \//);
  await anonymous.dispose();
  const context=await browser.newContext({viewport:{width:1440,height:1000}});
  await context.route('**/*',route=>{
    const url=new URL(route.request().url());
    if(url.protocol==='http:' && ['127.0.0.1'].includes(url.hostname) && ['13000','13001','13004'].includes(url.port))return route.continue();
    external.push(url.hostname);return route.abort();
  });
  const page=await context.newPage();page.on('pageerror',e=>errors.push(e.name));
  stage='operator_login';
  await page.goto(ops+'/login');await page.getByLabel('邮箱',{exact:true}).fill(process.env.INTEGRATION_OWNER_EMAIL);
  await page.getByLabel('密码',{exact:true}).fill(process.env.INTEGRATION_OWNER_PASSWORD);
  await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page).toHaveURL(ops+'/');
  passed('real_operator_browser_login');
  stage='initial_preview';
  const response=await page.goto(preview);assert.match(response.headers()['x-robots-tag'],/noindex/);
  await expect(page.locator('h1')).toHaveText('真实数据联调预览');await expect(page.locator('.card')).toHaveCount(24);
  await expect(page.locator('#counts')).toContainText('4,196');await expect(page.locator('#counts')).toContainText('4,066');
  passed('postgres_counts_and_server_pagination');
  const idsFirst=await page.locator('.card').evaluateAll(cards=>cards.map(c=>c.dataset.id));
  await page.locator('#next').click();await expect(page.locator('#page')).toHaveText('2 / 175');
  const idsSecond=await page.locator('.card').evaluateAll(cards=>cards.map(c=>c.dataset.id));
  assert.equal(idsSecond.some(id=>idsFirst.includes(id)),false);passed('pagination_no_overlap');
  const data=(await (await context.request.get(preview+'/api/performers')).json());
  assert.deepEqual(data.counts,{performers:4196,avatars:130,without_avatar:4066});
  stage='complete_database_projection';
  const actual=[];
  for(let index=1;index<=175;index++) {
    const response=await context.request.get(`${preview}/api/performers?page=${index}`);assert.equal(response.status(),200);
    const data=await response.json();assert.equal(data.total,4196);assert.ok(data.items.length<=24);actual.push(...data.items);
  }
  assert.equal(actual.length,4196);assert.equal(new Set(actual.map(p=>p.performer_id)).size,4196);
  const byID=new Map(actual.map(p=>[p.performer_id,p]));
  for(const expected of records)for(const key of Object.keys(expected))assert.deepEqual(byID.get(expected.performer_id)[key],expected[key]);
  assert.equal(actual.filter(p=>p.avatar).length,130);passed('all_4196_records_match_source_export');
  stage='all_avatar_decodes';
  await page.locator('#avatar').selectOption('yes');await page.getByRole('button',{name:'查询',exact:true}).click();
  let decoded=0;
  for(let index=1;index<=6;index++) {
    await expect(page.locator('#page')).toHaveText(`${index} / 6`);
    const expectedCount=index===6?10:24;await expect(page.locator('.card img')).toHaveCount(expectedCount);
    const sizes=await page.locator('.card img').evaluateAll(async imgs=>Promise.all(imgs.map(async img=>{await img.decode();return [img.naturalWidth,img.naturalHeight];})));
    sizes.forEach(size=>assert.deepEqual(size,[125,125]));decoded+=sizes.length;
    if(index===1)await page.screenshot({path:path.join(out,'preview-desktop.png')});
    if(index!==6)await page.locator('#next').click();
  }
  assert.equal(decoded,130);passed('all_local_avatars_decoded',decoded);
  stage='sample_details';
  for(const hasAvatar of [true,false]) {
    const sample=records.filter(p=>withAvatars.has(p.performer_id)===hasAvatar).slice(0,10);
    for(const expected of sample) {
      await page.locator('#avatar').selectOption('all');await page.locator('#query').fill(expected.external_id);
      await page.getByRole('button',{name:'查询',exact:true}).click();
      const card=page.locator(`[data-id="${expected.performer_id}"]`);await expect(card).toBeVisible();await expect(card.locator('h2')).toHaveText(expected.name);
      if(!hasAvatar)await expect(card.locator('.placeholder')).toHaveText('暂无头像');
      await card.getByRole('button',{name:'查看详情'}).click();await expect(page.locator('#detail')).toBeVisible();
      await expect(page.locator('#detail')).toContainText(expected.current_content_hash);
      if(hasAvatar)await page.locator('#detail img').evaluate(img=>img.decode());
      assert.doesNotMatch(await page.locator('#detail').innerText(),/\/root\/|[A-Z]:\\/);
      await page.locator('#close').click();
    }
  }
  passed('ten_with_and_ten_without_avatar_details',{with_avatar:10,without_avatar:10});
  stage='multilingual_search';
  for(const [label,regex] of [['han',/[\u4e00-\u9fff]/],['japanese',/[\u3040-\u30ff]/],['latin',/[a-zA-Z]/]]) {
    const sample=records.find(p=>regex.test(p.name));assert.ok(sample);
    await page.locator('#query').fill(sample.name);await page.getByRole('button',{name:'查询',exact:true}).click();
    await expect(page.locator(`[data-id="${sample.performer_id}"]`)).toBeVisible();passed(`search_${label}`);
  }
  for(const term of ['definitely-no-match-000',"' OR 1=1 --",'<script>alert(1)</script>','%_']) {
    const response=await context.request.get(preview+'/api/performers?q='+encodeURIComponent(term));assert.equal(response.status(),200);
    assert.equal((await response.json()).total,0);
  }
  for(const suffix of ['?page=0','?page=-1','?page=10001','?avatar=bad','/not-a-uuid'])assert.equal((await context.request.get(preview+'/api/performers'+suffix)).status(),400);
  assert.equal((await context.request.get(preview+'/api/performers?page=176')).status(),200);
  assert.equal((await context.request.get(preview+'/media/missing.jpg')).status(),404);
  passed('empty_invalid_injection_and_missing_media_cases');
  await page.locator('#query').fill('definitely-no-match-000');await page.getByRole('button',{name:'查询',exact:true}).click();await expect(page.locator('#result')).toHaveText('没有匹配的资料');
  await page.locator('#query').fill('');await page.locator('#avatar').selectOption('no');await page.getByRole('button',{name:'查询',exact:true}).click();await expect(page.locator('#cards .placeholder')).toHaveCount(24);
  await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await page.screenshot({path:path.join(out,'preview-mobile.png')});passed('empty_state_and_mobile_no_overflow');
  assert.deepEqual(external,[]);assert.deepEqual(errors,[]);passed('preview_no_external_requests_or_runtime_errors');
  stage='public_site_smoke';
  const publicContext=await browser.newContext({viewport:{width:1440,height:1000}});const publicPage=await publicContext.newPage();
  for(const route of ['/','/performers','/search?type=performer&q=integration-no-match-000']) {
    const response=await publicPage.goto(display+route);assert.equal(response.status(),200);await publicPage.locator('main').last().waitFor();
    assert.deepEqual(streamedRenderFailureSignals(await publicPage.content()),[]);
  }
  const publicSearch=await publicContext.request.get(display+'/api/v1/search/performers?q='+encodeURIComponent(records[0].name));
  assert.equal(publicSearch.status(),200);assert.equal((await publicSearch.json()).items.some(p=>byID.has(p.id)),false);
  passed('existing_public_pages_no_server_error_and_staging_not_searchable');
  report.status='passed';
} catch(error) {
  report.status='failed';report.failed_stage=stage;report.failures.push(String(error.message).slice(0,1000));process.exitCode=1;
} finally {
  await browser.close();writeFileSync(path.join(out,'browser-report.json'),JSON.stringify(report,null,2)+'\n');
  console.log(JSON.stringify({status:report.status,stage,checks:Object.keys(report.checks).length,failures:report.failures}));
}
