import assert from 'node:assert/strict';
import {writeFileSync} from 'node:fs';
import {chromium, expect, request} from '@playwright/test';

// Public probes are anonymous. Credentials and session cookies stay on loopback.
const publicPreview='http://117.72.170.254:13004';
const ops='http://127.0.0.1:13001', preview='http://127.0.0.1:13004';
assert.ok(process.env.INTEGRATION_OWNER_EMAIL && process.env.INTEGRATION_OWNER_PASSWORD);
const report={status:'running',checked_at:new Date().toISOString(),preview:publicPreview,checks:{},
  transport:'HTTP test environment only',authenticated_test_origin:preview,
  limitation:'Public probes originate on the server; no off-server reachability test or credential-bearing public HTTP request.'};
const anonymous=await request.newContext();
const browser=await chromium.launch({channel:'chrome',headless:true});
const context=await browser.newContext();
let loggedIn=false;
try {
  for(const route of ['/','/api/performers','/preview.js','/preview.css','/media/test.jpg']) {
    const response=await anonymous.get(publicPreview+route);
    assert.equal(response.status(),401);assert.match(response.headers()['x-robots-tag'],/noindex/);
  }
  assert.equal((await anonymous.get(publicPreview+'/api/performers',{headers:{Cookie:'sd_session=forged'}})).status(),401);
  report.checks.anonymous_and_forged_session_denied=true;
  const page=await context.newPage();
  await page.goto(ops+'/login');
  await page.getByLabel('邮箱',{exact:true}).fill(process.env.INTEGRATION_OWNER_EMAIL);
  await page.getByLabel('密码',{exact:true}).fill(process.env.INTEGRATION_OWNER_PASSWORD);
  await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page).toHaveURL(ops+'/');
  loggedIn=true;
  assert.equal((await page.goto(preview)).status(),200);
  await expect(page.locator('#cards .card')).toHaveCount(24);
  const response=await context.request.get(preview+'/api/performers?avatar=yes');
  assert.equal(response.status(),200);
  const data=await response.json();assert.deepEqual(data.counts,{performers:4196,avatars:130,without_avatar:4066});
  report.checks.loopback_login_and_shared_session=true;report.counts=data.counts;
  await page.locator('#avatar').selectOption('yes');await page.getByRole('button',{name:'查询',exact:true}).click();
  await expect(page.locator('#cards img')).toHaveCount(24);
  const dimensions=await page.locator('#cards img').evaluateAll(async imgs=>Promise.all(imgs.map(async img=>{await img.decode();return [img.naturalWidth,img.naturalHeight];})));
  dimensions.forEach(size=>assert.deepEqual(size,[125,125]));
  report.checks.loopback_preview_first_24_avatars_decoded=true;
  const mediaPath=`/media/${data.items[0].avatar.media_candidate_id}.jpg`;
  assert.equal((await anonymous.get(publicPreview+mediaPath)).status(),401);
  assert.equal((await context.request.get(preview+mediaPath)).status(),200);
  report.checks.real_avatar_requires_operator_session=true;
  assert.equal((await context.request.post(ops+'/api/v1/auth/logout',{headers:{Origin:ops}})).status(),204);
  loggedIn=false;
  assert.equal((await context.request.get(preview+'/api/performers')).status(),401);
  report.checks.logout_revokes_preview_access=true;report.status='passed';
} catch(error) {
  report.status='failed';report.error=String(error.message).slice(0,1000);process.exitCode=1;
} finally {
  if(loggedIn)await context.request.post(ops+'/api/v1/auth/logout',{headers:{Origin:ops}}).catch(()=>{});
  await browser.close();await anonymous.dispose();
  writeFileSync('docs/evidence/collector-integration-2026-09-19/public-preview-report.json',JSON.stringify(report,null,2)+'\n');
  console.log(JSON.stringify(report));
}
