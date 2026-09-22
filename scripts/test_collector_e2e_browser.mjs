import assert from 'node:assert/strict';
import {readFileSync, writeFileSync} from 'node:fs';
import path from 'node:path';
import {chromium, expect, request} from '@playwright/test';

const api = process.env.COLLECTOR_E2E_API_URL;
const ops = process.env.COLLECTOR_E2E_OPS_URL;
const password = process.env.COLLECTOR_E2E_PASSWORD;
const output = process.env.COLLECTOR_E2E_OUTPUT;
assert.ok(api && ops && password && output, 'acceptance runtime required');
for (const base of [api,ops]) assert.equal(new URL(base).hostname,'127.0.0.1');
const expected = JSON.parse(readFileSync(path.join(output,'expected.json'),'utf8'));
const report = {status:'running',checks:{},failures:[]};
const browser = await chromium.launch({channel:'chrome',headless:true});
const contexts = [];
const urls = ['/admin/v1/collector/batches','/admin/v1/collector/candidates'];
async function loggedIn(role) {
  const context = await request.newContext({baseURL:api}); contexts.push(context);
  const login = await context.post('/api/v1/auth/login',{data:{email:`collector-${role}@example.test`,password}});
  assert.equal(login.status(),200,`${role} login`);
  return context;
}
function pass(name,value=true){report.checks[name]=value;console.log(`PASS ${name}`);}
try {
  const anonymous = await request.newContext({baseURL:api});contexts.push(anonymous);
  for(const url of urls) assert.equal((await anonymous.get(url)).status(),401);
  pass('anonymous_denied');
  for(const role of ['user','editor','admin']) {
    const client = await loggedIn(role);
    for(const url of urls) assert.equal((await client.get(url)).status(),role==='user'?403:200);
    await client.post('/api/v1/auth/logout');
  }
  pass('real_role_matrix');
  const owner = await loggedIn('owner');
  const batchResponse=await owner.get(urls[0]);assert.equal(batchResponse.status(),200);
  const batchData=await batchResponse.json();assert.equal(batchData.total,9);
  assert.equal(batchData.items.reduce((sum,b)=>sum+b.input_count,0),4196);
  assert.equal(batchData.items.reduce((sum,b)=>sum+b.pending_count,0),4196);
  pass('nine_batches_and_pending_counts');
  const all=[];
  for(let offset=0;offset<4196;offset+=100){
    const response=await owner.get(`${urls[1]}?limit=100&offset=${offset}`);assert.equal(response.status(),200);
    assert.equal(response.headers()['cache-control'],'no-store');
    const data=await response.json();assert.equal(data.total,4196);all.push(...data.items);
  }
  assert.equal(all.length,4196);assert.equal(new Set(all.map(p=>p.candidate_id)).size,4196);
  const byExternal=new Map(all.map(p=>[p.external_id,p]));
  for(const expectedPerson of expected){
    const actual=byExternal.get(expectedPerson.external_id);assert.ok(actual);
    for(const [key,value] of Object.entries(expectedPerson))assert.deepEqual(actual[key],value,key);
    assert.equal(actual.candidate_status,'pending');assert.equal(actual.rights_status,'needs_review');
    assert.equal(actual.publication_status,'staging');assert.equal(actual.adult_status,'unknown');
  }
  pass('all_4196_candidates_match_archive');
  const selected=all[0];
  const detailURL=`${urls[1]}/${selected.candidate_id}`;
  assert.equal((await anonymous.get(detailURL)).status(),401);
  const ordinary=await loggedIn('user');assert.equal((await ordinary.get(detailURL)).status(),403);
  const detail=await owner.get(detailURL);assert.equal(detail.status(),200);assert.deepEqual(await detail.json(),selected);
  await ordinary.post('/api/v1/auth/logout');
  pass('detail_and_detail_permissions');
  for(const [query,want] of [['limit=0',400],['offset=-1',400],['status=published',400],['source_id=bad',400],['q=%27%25_%3B--',200]]){
    assert.equal((await owner.get(`${urls[1]}?${query}`)).status(),want);
  }
  const search=await owner.get(`${urls[1]}?q=${encodeURIComponent(selected.name)}`);assert.equal(search.status(),200);
  assert.ok((await search.json()).items.some(p=>p.external_id===selected.external_id));
  const empty=await owner.get(`${urls[1]}?q=collector-no-match-69c137590`);assert.equal((await empty.json()).total,0);
  const batchFiltered=await owner.get(`${urls[1]}?batch_id=${batchData.items[0].batch_id}&limit=100`);
  assert.equal((await batchFiltered.json()).total,batchData.items[0].candidate_count);
  pass('search_empty_filters_and_invalid_inputs');
  for(const url of ['/api/v1/performers','/api/v1/search/performers?q='+encodeURIComponent(selected.name)]){
    const response=await anonymous.get(url);assert.equal(response.status(),200);
    assert.equal((await response.json()).items.length,0);
  }
  pass('public_api_excludes_staged_candidates');
  const context=await browser.newContext({viewport:{width:1440,height:1000}});contexts.push(context);
  const external=[],errors=[];
  await context.route('**/*',route=>{
    const url=new URL(route.request().url());
    if(url.hostname==='127.0.0.1' && [new URL(api).port,new URL(ops).port].includes(url.port))return route.continue();
    external.push(url.hostname);return route.abort();
  });
  const page=await context.newPage();page.on('pageerror',error=>errors.push(error.name));
  await page.goto(ops+'/collector');await expect(page).toHaveURL(/\/login/);
  await page.getByLabel('邮箱',{exact:true}).fill('collector-owner@example.test');
  await page.getByLabel('密码',{exact:true}).fill(password);
  await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page).toHaveURL(ops+'/');
  await page.goto(ops+'/collector');await expect(page.getByRole('heading',{level:1})).toContainText('采集');
  await expect(page.locator('main')).toContainText(/4,?196/);
  await page.screenshot({path:path.join(output,'collector-desktop.png'),fullPage:true});
  await page.getByRole('navigation',{name:'候选分页'}).getByRole('link',{name:'下一页'}).click();
  await expect(page).toHaveURL(/offset=25/);
  await page.locator('a:visible').filter({hasText:'核对资料'}).first().click();
  await expect(page).toHaveURL(/\/collector\/[^?]+\?offset=25/);
  await page.getByRole('link',{name:'返回候选列表'}).click();
  await expect(page).toHaveURL(/\/collector\?offset=25#candidates/);
  await page.locator('input[name="q"]').fill(selected.name);
  await page.getByRole('button',{name:'查询',exact:true}).click();
  await expect(page.locator('#candidates')).toContainText(selected.name);
  await page.getByLabel('候选状态',{exact:true}).selectOption('pending');
  await page.getByRole('button',{name:'查询',exact:true}).click();
  await page.locator('a:visible').filter({hasText:'核对资料'}).first().click();
  await page.getByRole('link',{name:'返回候选列表'}).click();
  await expect(page.locator('input[name="q"]')).toHaveValue(selected.name);
  await expect(page.getByLabel('候选状态',{exact:true})).toHaveValue('pending');
  pass('browser_detail_returns_to_query_status_and_page');
  await page.locator('input[name="q"]').fill('collector-no-match-69c137590');
  await page.getByRole('button',{name:'查询',exact:true}).click();
  await expect(page.locator('#candidates')).toContainText('没有找到候选资料');
  pass('browser_pagination_search_empty');
  await page.goto(ops+`/collector/${selected.candidate_id}`);
  await expect(page.locator('main')).toContainText(selected.name);
  await expect(page.locator('main')).toContainText('待');
  await page.screenshot({path:path.join(output,'collector-detail.png'),fullPage:true});
  await page.setViewportSize({width:390,height:844});await page.goto(ops+'/collector');
  await expect(page.locator('main')).toContainText(/4,?196/);
  await expect(page.locator('a:visible').filter({hasText:'核对资料'}).first()).toBeInViewport();
  await page.screenshot({path:path.join(output,'collector-mobile.png'),fullPage:true});
  const mobileSize=await page.evaluate(()=>({viewport:window.innerWidth,document:document.documentElement.scrollWidth,
    overflow:[...document.querySelectorAll('body *')].filter(e=>e.getBoundingClientRect().right>window.innerWidth+1).slice(0,12).map(e=>({tag:e.tagName,class:e.className,right:e.getBoundingClientRect().right}))}));
  report.checks.mobile_dimensions=mobileSize;
  assert.ok(mobileSize.document<=mobileSize.viewport+1,'mobile overflow');
  const responsive=[];
  for(const width of [320,390,760,761,768,1024,1440]){
    await page.setViewportSize({width,height:900});
    const size=await page.evaluate(()=>({viewport:innerWidth,document:document.documentElement.scrollWidth}));
    assert.ok(size.document<=size.viewport+1,`overflow at ${width}`);
    responsive.push(size);
  }
  report.checks.responsive_dimensions=responsive;
  await page.setViewportSize({width:390,height:844});
  await page.locator('a:visible').filter({hasText:'核对资料'}).first().click();
  await expect(page.getByRole('link',{name:'返回候选列表'})).toBeVisible();
  await page.getByRole('link',{name:'返回候选列表'}).click();
  await expect(page.getByRole('heading',{name:'人物候选',exact:true})).toBeVisible();
  pass('mobile_primary_action_and_return_are_accessible');
  assert.equal(external.length,0);assert.equal(errors.length,0);
  pass('real_browser_login_list_detail_desktop_mobile',{external_requests:external.length,runtime_errors:errors.length});
  await owner.post('/api/v1/auth/logout');assert.equal((await owner.get(urls[1])).status(),401);
  await page.getByRole('button',{name:'退出登录',exact:true}).click();
  await expect(page).toHaveURL(/\/login/);
  await page.goto(ops+'/collector');await expect(page).toHaveURL(/\/login/);
  pass('logout_revokes_access');report.status='passed';
} catch(error){report.status='failed';report.failures.push(error.stack);throw error;}
finally{
  writeFileSync(path.join(output,'browser-report.json'),JSON.stringify(report,null,2)+'\n');
  for(const context of contexts)await context.dispose?.();
  await browser.close();
}
