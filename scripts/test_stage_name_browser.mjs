import assert from 'node:assert/strict';
import {mkdirSync,writeFileSync} from 'node:fs';
import {chromium,expect} from '@playwright/test';

const ops='http://127.0.0.1:13001',preview='http://127.0.0.1:13004',display='http://127.0.0.1:13000';
const output='docs/evidence/stage-name-search-2026-09-19';mkdirSync(output,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true});
const context=await browser.newContext(),page=await context.newPage();
const report={checked_at:new Date().toISOString(),status:'running',checks:{}};
try{
  await page.goto(display+'/search?type=performer');
  await expect(page.locator('#result-query')).toHaveAttribute('placeholder','输入艺名、曾用名或别名');
  await page.locator('#result-query').fill('三上悠亜');await page.getByRole('button',{name:'查询',exact:true}).click();
  await expect(page).toHaveURL(/type=performer/);
  await expect(page.locator('main')).toContainText('没有找到相关已发布人物资料');
  report.checks.public_stage_name_form_preserves_entity_scope=true;
  report.checks.unpublished_real_staging_not_leaked=true;
  await page.screenshot({path:output+'/public-search.png',fullPage:true});
  await page.goto(ops+'/login');await page.getByLabel('邮箱',{exact:true}).fill(process.env.INTEGRATION_OWNER_EMAIL);
  await page.getByLabel('密码',{exact:true}).fill(process.env.INTEGRATION_OWNER_PASSWORD);
  await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page).toHaveURL(ops+'/');
  await page.goto(preview);await expect(page.getByLabel('艺名 / 已收录别名 / 来源 ID')).toBeVisible();
  for(const [query,total] of [['三上悠亜',1],['三上',4],['AIKA',5],['aika',5]]){
    await page.locator('#query').fill(query);await page.getByRole('button',{name:'查询',exact:true}).click();
    await expect(page.locator('#result')).toContainText(`共 ${total} 条`);
    if(query.toLowerCase()==='aika')await expect(page.locator('#cards h2').filter({hasText:/^AIKA$/})).toHaveCount(1);
  }
  const aika=page.locator('#cards article').filter({has:page.getByRole('heading',{name:'AIKA',exact:true})});
  await aika.getByRole('button',{name:'查看详情'}).click();
  await expect(page.locator('#detail')).toContainText('已收录别名 / 曾用名');
  await expect(page.locator('#detail')).toContainText('来源未提供');
  await page.screenshot({path:output+'/staging-stage-name.png',fullPage:true});
  report.checks.real_stage_name_full_partial_and_case_insensitive=true;
  report.checks.missing_alias_not_fabricated=true;
  const data=await (await context.request.get(preview+'/api/performers')).json();assert.equal(data.total,4196);
  const traffic=await (await context.request.get(ops+'/admin/v1/traffic?range=all')).json();assert.equal(traffic.page_views,0);
  report.checks.staging_4196_and_public_pv_zero_unchanged=true;report.status='passed';
}catch(error){report.status='failed';report.error=String(error.message).slice(0,1500);process.exitCode=1;}
finally{await context.request.post(ops+'/api/v1/auth/logout',{headers:{Origin:ops}}).catch(()=>{});await browser.close();writeFileSync(output+'/browser.json',JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));}
