import assert from 'node:assert/strict';
import { mkdirSync, writeFileSync } from 'node:fs';
import { chromium, expect } from '@playwright/test';

// Read-only live acceptance except normal login/logout security audit events.
// No fixture accounts, catalog entities or advertisements are created.
const ops='http://127.0.0.1:13001',display='http://127.0.0.1:13000';
const out='docs/evidence/cleanup-search-traffic-2026-09-19';
mkdirSync(out,{recursive:true});
const report={checked_at:new Date().toISOString(),status:'running',checks:{},reports:[]};
const browser=await chromium.launch({channel:'chrome',headless:true});
const context=await browser.newContext({viewport:{width:1440,height:1000}}),page=await context.newPage();
try {
  assert.equal((await context.request.get(ops+'/admin/v1/traffic')).status(),401);
  await page.goto(ops+'/traffic');await expect(page).toHaveURL(/\/login/);
  report.checks.anonymous_api_and_page_protected=true;
  await page.getByLabel('邮箱',{exact:true}).fill(process.env.INTEGRATION_OWNER_EMAIL);
  await page.getByLabel('密码',{exact:true}).fill(process.env.INTEGRATION_OWNER_PASSWORD);
  await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page).toHaveURL(ops+'/');
  await page.getByRole('link',{name:'访问统计',exact:true}).click();
  await expect(page.getByRole('heading',{name:'访问统计',exact:true})).toBeVisible();
  await expect(page.getByRole('alert')).toHaveCount(0);
  for(const range of ['7d','30d','all']){
    const response=await context.request.get(ops+'/admin/v1/traffic?range='+range);assert.equal(response.status(),200);
    assert.match(response.headers()['cache-control'],/no-store/);
    const data=await response.json();assert.equal(data.unique_visitors,null);
    assert.equal(data.page_views,0);assert.equal(data.logged_in_user_views,0);assert.equal(data.active_registered_users,0);
    assert.equal(data.hot_content.length,0);assert.equal(data.users.length,0);assert.equal(data.daily.length,range==='7d'?7:30);
    assert.ok(data.daily.every(d=>d.page_views===0));report.reports.push(data);
    await page.getByRole('link',{name:range==='7d'?'近 7 天':range==='30d'?'近 30 天':'历史累计',exact:true}).click();
    await expect(page.locator('progress')).toHaveCount(data.daily.length);
  }
  assert.equal((await context.request.get(ops+'/admin/v1/traffic?range=invalid')).status(),400);
  await page.screenshot({path:out+'/traffic-desktop.png',fullPage:true});
  report.checks.live_zero_data_and_ranges=true;
  await page.setViewportSize({width:390,height:844});
  await expect(page.getByRole('heading',{name:'访问统计',exact:true})).toBeVisible();
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+1),'mobile document overflow');
  await page.screenshot({path:out+'/traffic-mobile.png',fullPage:true});report.checks.mobile_no_document_overflow=true;
  for(const route of ['/','/latest','/popular','/performers']){
    const response=await page.goto(display+route);assert.equal(response.status(),200);
    await expect(page.locator('[data-ad-state="ready"]')).toHaveCount(0);
    assert.doesNotMatch(await page.locator('body').innerText(),/测试作品|测试人物|模拟广告/);
  }
  report.checks.public_pages_no_synthetic_content_or_ads=true;
  for(const route of ['/api/v1/works/test-001','/api/v1/performers/fixture-performer','/api/v1/studios/fixture-studio'])assert.equal((await context.request.get(display+route)).status(),404);
  const removed=await context.request.post(display+'/api/v1/metrics/page-view',{headers:{Origin:display},data:{content_type:'work',content_id:'10000000-0000-4000-8000-000000000001'}});
  assert.equal(removed.status(),404);
  const after=await (await context.request.get(ops+'/admin/v1/traffic?range=all')).json();assert.equal(after.page_views,0);
  report.checks.deleted_details_404_and_list_browsing_does_not_count=true;
  const preview=await context.request.get('http://127.0.0.1:13004/api/performers');assert.equal(preview.status(),200);assert.equal((await preview.json()).total,4196);
  report.checks.real_staging_4196_unchanged=true;
  report.status='passed';
}catch(error){report.status='failed';report.error=String(error.message).slice(0,1800);process.exitCode=1;}
finally{
  await context.request.post(ops+'/api/v1/auth/logout',{headers:{Origin:ops}}).catch(()=>{});
  await browser.close();writeFileSync(out+'/traffic-browser.json',JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
}
