import assert from 'node:assert/strict';
import {mkdirSync,writeFileSync} from 'node:fs';
import {chromium,expect} from '@playwright/test';

const phase=process.argv[2]??'before';assert.ok(['before','after'].includes(phase));
const browser=await chromium.launch({channel:'chrome',headless:true});
const context=await browser.newContext(),page=await context.newPage();
const ops='http://127.0.0.1:13001',preview='http://127.0.0.1:13004',display='http://127.0.0.1:13000';
const report={checked_at:new Date().toISOString(),phase,status:'running',queries:[],checks:{}};
try {
  await page.goto(ops+'/login');await page.getByLabel('邮箱',{exact:true}).fill(process.env.INTEGRATION_OWNER_EMAIL);
  await page.getByLabel('密码',{exact:true}).fill(process.env.INTEGRATION_OWNER_PASSWORD);
  await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page).toHaveURL(ops+'/');
  for(const query of ['三上','三上悠亜','三上悠亚','橋本','桥本','深田','深田咏美','明日花']) {
    const response=await context.request.get(preview+'/api/performers?q='+encodeURIComponent(query));assert.equal(response.status(),200);
    const data=await response.json();report.queries.push({surface:'staging',query,total:data.total,names:data.items.map(p=>p.name)});
  }
  await page.goto(preview);await page.locator('#query').fill('三上');await page.getByRole('button',{name:'查询',exact:true}).click();
  await expect(page.locator('#result')).toContainText('共 4 条');await expect(page.locator('#cards')).toContainText('三上悠亜');
  report.checks.actual_browser_chinese_input=true;
  for(const [entity,query] of [['works','城市'],['performers','测试人物'],['studios','测试厂牌']]) {
    const response=await context.request.get(`${display}/api/v1/search/${entity}?q=${encodeURIComponent(query)}`);
    assert.equal(response.status(),200);const data=await response.json();
    if(phase==='after')assert.equal(data.items.length,0);
    report.queries.push({surface:'public-'+entity,query,returned:data.items.length});
  }
  report.checks.public_chinese_queries_return_http_200=true;
  report.limitations=['Exact stored Chinese/Han substrings work; simplified/traditional/Japanese variant folding and translated-name aliases are not currently provided.','Real imported performers are staging-only and intentionally absent from public search.'];
  report.status='passed';
} catch(error) {report.status='failed';report.error=String(error.message).slice(0,900);process.exitCode=1;}
finally {
  await context.request.post(ops+'/api/v1/auth/logout',{headers:{Origin:ops}}).catch(()=>{});
  await browser.close();const out='docs/evidence/cleanup-search-traffic-2026-09-19';mkdirSync(out,{recursive:true});
  writeFileSync(`${out}/chinese-${phase}.json`,JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
}
