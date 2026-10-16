import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {existsSync,mkdirSync,readFileSync,writeFileSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {chromium,expect} from '@playwright/test';
import {resolvePython} from './run_python.mjs';
import {themes,pages,previewURL} from '../docs/design-review-2026-10/data.mjs';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const lab=path.join(root,'docs/design-review-2026-10');
const output=path.join(root,'.cache/design-review');
const options={mode:'smoke',themes:'',workers:4};
for(let i=2;i<process.argv.length;i+=2){const key=process.argv[i]?.replace(/^--/,'');if(!['mode','themes','workers','base-url'].includes(key)||!process.argv[i+1])throw new Error('Usage: --mode smoke|matrix|narrow|capture|all [--themes 01,02] [--workers 1..4] [--base-url http://127.0.0.1:PORT]');options[key]=process.argv[i+1];}
assert.ok(['smoke','matrix','narrow','capture','all'].includes(options.mode),'Unknown mode');
options.workers=Number(options.workers);assert.ok(Number.isInteger(options.workers)&&options.workers>=1&&options.workers<=4);
const ids=options.themes?options.themes.split(','):themes.map(t=>t.id);
assert.ok(ids.every(id=>themes.some(t=>t.id===id)),'Unknown theme');
const executable=process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || [chromium.executablePath(),path.join(root,'.cache/ms-playwright/chromium_headless_shell-1243/chrome-headless-shell-linux64/chrome-headless-shell')].find(existsSync);
assert.ok(executable&&existsSync(executable),'Set PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH to an installed Chromium executable');
mkdirSync(output,{recursive:true});
let server,browser,origin,count=0;
const started=new Date().toISOString();
const checks=[];

function startServer(){
  server=spawn(resolvePython(),['scripts/serve_design_review.py','--port','0'],{cwd:root,stdio:['ignore','pipe','pipe']});
  return new Promise((resolve,reject)=>{let text='';const timer=setTimeout(()=>reject(new Error('Preview startup timeout')),10000);server.stdout.on('data',chunk=>{text+=String(chunk);const match=text.match(/http:\/\/127\.0\.0\.1:\d+/);if(match){clearTimeout(timer);resolve(match[0]);}});server.once('error',error=>{clearTimeout(timer);reject(error);});server.once('exit',code=>{clearTimeout(timer);reject(new Error(`Preview exited: ${code}`));});});
}
function url(theme='01',surface='public',page='home',state='ready'){return `${origin}/${previewURL(theme,surface,page,state)}`;}
function overflow(){
  const width=document.documentElement.clientWidth;
  if(document.documentElement.scrollWidth<=width+1)return [];
  return [...document.querySelectorAll('body *')].filter(el=>{const r=el.getBoundingClientRect(),style=getComputedStyle(el);return r.width>0&&r.right>width+2&&style.position!=='fixed'&&!el.closest('.frame-window')&&!el.classList.contains('sr-only');}).slice(0,8).map(el=>({tag:el.tagName,class:el.className,right:el.getBoundingClientRect().right,width}));
}
async function ready(page,target){
  const response=await page.goto(target,{waitUntil:'networkidle'});assert.equal(response.status(),200,target);
  await expect(page.locator('h1:visible')).toHaveCount(1);
  assert.deepEqual(await page.evaluate(overflow),[],`Overflow at ${target}`);
}
async function stablePreviews(page){
  const frames=await page.locator('iframe').all();
  for(let i=0;i<frames.length;i++){
    const target=new URL(await frames[i].getAttribute('src'),origin);
    const preview=page.frameLocator('iframe').nth(i);
    await expect(preview.locator('body')).toHaveAttribute('data-theme',target.searchParams.get('theme'));
    await expect(preview.locator('body')).toHaveAttribute('data-page',target.searchParams.get('page'));
    await expect(preview.locator('h1')).toBeVisible();
  }
}
async function contextFor(viewport={width:1440,height:1000}){
  const context=await browser.newContext({viewport,reducedMotion:'reduce',acceptDownloads:true});
  const issues=[];
  await context.route('**/*',route=>{const request=new URL(route.request().url());if(['http:','https:'].includes(request.protocol)&&request.origin!==origin){issues.push(`External request: ${request.href}`);return route.abort();}return route.continue();});
  context.on('page',page=>page.on('pageerror',error=>issues.push(error.message)));
  return {context,issues,page:await context.newPage()};
}
async function httpChecks(){
  for(const route of ['/','/preview.html','/data.mjs','/robots.txt','/healthz']){const res=await fetch(origin+route);assert.equal(res.status,200);assert.equal(res.headers.get('cache-control'),'no-store');assert.match(res.headers.get('x-robots-tag'),/noindex/);assert.match(res.headers.get('content-security-policy'),/connect-src 'none'/);}
  for(const route of ['/.env','/.git/config','/package.json','/../package.json','/%2e%2e/package.json','/README.md','/thumbs/../../package.json'])assert.equal((await fetch(origin+route)).status,404,route);
  checks.push('Static allowlist, no-store/noindex/CSP and repository-path rejection');
}
async function smoke(){
  await httpChecks();
  const {context,page,issues}=await contextFor();
  try {
    // webapp-testing reconnaissance: wait for hydration/network idle, inspect, then act.
    await ready(page,url());await page.screenshot({path:path.join(output,'initial-public.png'),fullPage:true});
    await expect(page.getByRole('link',{name:'Explore the archive',exact:true})).toHaveCount(1);
    await ready(page,url('01','public','search'));
    await page.getByRole('searchbox',{name:'Search the archive'}).fill('SD-026');await page.getByRole('button',{name:'Search',exact:true}).click();await expect(page.locator('.work-card')).toHaveCount(1);
    await page.getByRole('button',{name:'Save After the rain',exact:true}).click();await expect(page.getByRole('button',{name:'Save After the rain',exact:true})).toHaveAttribute('aria-pressed','true');
    await page.getByRole('searchbox',{name:'Search the archive'}).fill('<img src=x onerror="alert(1)">');await page.getByRole('button',{name:'Search',exact:true}).click();await expect(page.locator('#results')).toContainText('No matching sample records');
    await page.getByRole('button',{name:'Reset filters',exact:true}).click();await expect(page.locator('.work-card')).toHaveCount(8);
    await page.getByLabel('Studio',{exact:true}).selectOption('Northlight');await expect(page.locator('.work-card')).toHaveCount(2);
    await ready(page,url('01','public','work'));const trigger=page.getByRole('button',{name:'Hide this record',exact:true});await trigger.click();await expect(page.getByRole('dialog')).toBeVisible();await page.keyboard.press('Escape');await expect(page.getByRole('dialog')).not.toBeVisible();await expect(trigger).toBeFocused();
    await ready(page,url('01','public','register'));await page.getByLabel('Email address',{exact:true}).fill('design@example.invalid');await page.getByLabel('Demo password (12+ characters)',{exact:true}).fill('fictional-password-only');await page.getByRole('checkbox').check();await page.getByRole('button',{name:'Preview account creation',exact:true}).click();await expect(page.locator('.form-status')).toContainText('Nothing was transmitted');
    await ready(page,url('01','public','age'));for(const [value,text] of [['unavailable','Verification unavailable'],['restricted','Not available in this region']]){await page.getByLabel('Review a verification state').selectOption(value);await expect(page.locator('#age-result')).toContainText(text);}
    await ready(page,url('01','public','cookies'));await expect(page.locator('#analytics-choice')).not.toBeChecked();await page.locator('#analytics-choice').check();await page.getByRole('button',{name:'Reject optional',exact:true}).click();await expect(page.locator('#analytics-choice')).not.toBeChecked();
    checks.push('Search/filter/save, inert untrusted text, confirmation focus, demo forms, age failure states and default-off optional tracking');
    await ready(page,url('01','ops','collector'));await page.getByRole('searchbox',{name:'Filter table'}).fill('After the rain');await expect(page.locator('tbody tr:visible')).toHaveCount(1);
    await page.setViewportSize({width:390,height:844});await ready(page,url('03','ops','catalog'));await page.locator('.mobile-menu summary').click();const safety=page.locator('.mobile-menu').getByRole('link',{name:'Rights & safety desk',exact:true});await expect(safety).toBeVisible();await safety.click();await page.waitForLoadState('networkidle');await expect(page.locator('h1')).toHaveText('Rights & safety desk');
    checks.push('Operations table filters and accessible mobile navigation to safety desk');
    await page.setViewportSize({width:1440,height:1000});await ready(page,origin+'/');await expect(page.locator('.theme-card')).toHaveCount(20);
    for(const mode of ['ops','mobile']){await page.locator('#gallery-mode').selectOption(mode);await expect(page.locator('.theme-card img').first()).toHaveAttribute('src',`thumbs/01-${mode}.jpg`);await page.getByRole('button',{name:'审核 01 午夜首映',exact:true}).click();await stablePreviews(page);await expect(page.locator('#surface-select')).toHaveValue(mode==='ops'?'ops':'public');await expect(page.locator('#viewport-select')).toHaveValue(mode==='mobile'?'mobile':'desktop');await page.getByRole('button',{name:'20 套总览',exact:true}).click();}await page.locator('#gallery-mode').selectOption('public');
    await page.getByRole('button',{name:'收藏 午夜首映',exact:true}).click();await expect(page.locator('#favorite-count')).toHaveText('1');await page.locator('#favorites-only').check();await expect(page.locator('.theme-card')).toHaveCount(1);await page.getByRole('button',{name:'进入审核',exact:false}).click();await page.frameLocator('iframe').locator('h1').waitFor();
    await page.locator('#note-text').fill('保留分屏。<script>not executable</script>');await page.locator('#rating-select').selectOption('shortlist');await page.reload({waitUntil:'networkidle'});await expect(page.locator('#note-text')).toHaveValue('保留分屏。<script>not executable</script>');await expect(page.locator('#rating-select')).toHaveValue('shortlist');
    await page.locator('#compare-toggle').check();await page.locator('#compare-select').selectOption('14');await expect(page.locator('iframe')).toHaveCount(2);await stablePreviews(page);await page.screenshot({path:path.join(output,'review-compare.png'),fullPage:true});await page.locator('#page-select').selectOption('search');for(const frame of await page.locator('iframe').all())await expect(frame).toHaveAttribute('src',/page=search/);await stablePreviews(page);
    await page.locator('#viewport-select').selectOption('mobile');await expect.poll(()=>page.locator('iframe').first().evaluate(el=>el.clientWidth)).toBe(390);
    await page.locator('#note-text').fill('手机需要紧凑一些');await page.locator('#rating-select').selectOption('revise');const downloaded=page.waitForEvent('download');await page.locator('#export-review').click();const download=await downloaded;const exported=JSON.parse(readFileSync(await download.path(),'utf8'));assert.equal(exported.notes.length,2);assert.deepEqual(exported.favorites,['01']);
    const payload={version:1,favorites:['02'],notes:[{theme:'01',surface:'public',page:'search',viewport:'mobile',state:'ready',rating:'shortlist',text:'合并意见'}]};
    await page.locator('#import-file').setInputFiles({name:'review.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(payload))});await expect(page.locator('#note-text')).toHaveValue('合并意见');
    await page.locator('#import-file').setInputFiles({name:'invalid.json',mimeType:'application/json',buffer:Buffer.from('{"version":99}')});await expect(page.locator('#lab-message')).toContainText('导入失败');await expect(page.locator('#note-text')).toHaveValue('合并意见');
    await page.locator('#surface-select').selectOption('ops');await expect(page.locator('#page-select option')).toHaveCount(26);await page.locator('#surface-select').selectOption('public');await expect(page.locator('#page-select option')).toHaveCount(28);await page.locator('#compare-toggle').uncheck();await page.frameLocator('iframe').getByRole('link',{name:'Explore the archive',exact:true}).click();await expect(page.locator('#page-select')).toHaveValue('search');
    checks.push('20-theme gallery, favorites, per-page notes, persistence, comparison, validated import/export and iframe navigation sync');
    await page.goto(origin+'/',{waitUntil:'networkidle'});await page.keyboard.press('Tab');await expect(page.locator('.skip')).toBeFocused();await page.keyboard.press('Enter');await expect(page.locator('#workspace')).toBeFocused();
    for(const width of [390,320]){await page.setViewportSize({width,height:844});await ready(page,origin+'/');await page.getByRole('button',{name:'页面审核',exact:true}).click();await stablePreviews(page);assert.deepEqual(await page.evaluate(overflow),[]);const canvas=await page.locator('.review-canvas').boundingBox(),notes=await page.locator('.note-panel').boundingBox();assert.ok(notes.y>=canvas.y+canvas.height-1,'Mobile notes should follow the preview');if(width===390)await page.screenshot({path:path.join(output,'review-mobile-audit.png'),fullPage:true});await page.getByRole('button',{name:'上线前检查',exact:true}).click();assert.deepEqual(await page.evaluate(overflow),[]);}
    assert.deepEqual(issues,[]);checks.push('Keyboard skip link, 320/390px reviewer shell, zero script exceptions and zero external requests');
  } finally {await context.close();}
  console.log('PASS interactive acceptance and isolation');
}
async function matrixTheme(id,narrow){
  const {context,page,issues}=await contextFor();let local=0;
  try {
    for(const width of narrow?[320]:[1440,390]){
      await page.setViewportSize({width,height:width===1440?1000:844});
      for(const [surface,list] of Object.entries(pages))for(const item of list){
        if(narrow&&!['home','search','work','catalog','rights','candidate','settings','login','report'].includes(item.id))continue;
        try {await ready(page,url(id,surface,item.id));await expect(page.locator('body')).toHaveAttribute('data-page',item.id);}catch(error){await page.screenshot({path:path.join(output,`failure-${id}-${surface}-${item.id}-${width}.png`),fullPage:true});throw error;}
        local++;
      }
      if(!narrow)for(const surface of ['public','ops'])for(const state of ['empty','loading','error','denied']){await ready(page,url(id,surface,'home',state));await expect(page.locator('.state-panel')).toHaveCount(1);local++;}
    }
    assert.deepEqual(issues,[]);console.log(`PASS ${id}: ${local} ${narrow?'320px':'desktop/mobile/state'} cases`);return local;
  }finally{await context.close();}
}
async function matrix(narrow){
  let next=0;
  const results=await Promise.allSettled(Array.from({length:options.workers},async()=>{let passed=0;while(next<ids.length){const id=ids[next++];passed+=await matrixTheme(id,narrow);}return passed;}));
  for(const result of results)if(result.status==='fulfilled')count+=result.value;
  const errors=results.filter(r=>r.status==='rejected').map(r=>r.reason);if(errors.length)throw new AggregateError(errors,'Design matrix failed');
}
async function capture(){
  const thumbs=path.join(lab,'thumbs');mkdirSync(thumbs,{recursive:true});const {context,page,issues}=await contextFor();
  try{
    for(const id of ids){for(const [name,surface,width,height] of [['public','public',1440,1000],['ops','ops',1440,1000],['mobile','public',390,844]]){await page.setViewportSize({width,height});await ready(page,url(id,surface));await page.screenshot({path:path.join(thumbs,`${id}-${name}.jpg`),type:'jpeg',quality:78});}console.log(`Captured ${id}: public / operations / mobile`);}
    for(const width of [1440,390]){await page.setViewportSize({width,height:width===1440?1000:844});await ready(page,origin+'/');await page.screenshot({path:path.join(output,`review-gallery-${width}.png`),fullPage:true});}
    await page.setViewportSize({width:1440,height:1000});await ready(page,`${origin}/?view=review&theme=01`);await page.locator('#compare-toggle').check();await page.locator('#compare-select').selectOption('14');await stablePreviews(page);await page.screenshot({path:path.join(output,'review-compare.png'),fullPage:true});
    assert.deepEqual(issues,[]);
  }finally{await context.close();}
}
try{
  if(options['base-url']){const parsed=new URL(options['base-url']);assert.ok(['localhost','127.0.0.1'].includes(parsed.hostname)&&parsed.protocol==='http:'&&!parsed.username&&!parsed.password&&parsed.pathname==='/'&&!parsed.search&&!parsed.hash,'Only loopback HTTP origins are accepted');origin=parsed.origin;}else origin=await startServer();
  browser=await chromium.launch({executablePath:executable,headless:true});
  if(['capture','all'].includes(options.mode))await capture();
  if(['smoke','all'].includes(options.mode))await smoke();
  if(['matrix','all'].includes(options.mode))await matrix(false);
  if(['narrow','all'].includes(options.mode))await matrix(true);
  const report={started,completed:new Date().toISOString(),mode:options.mode,themes:ids,renderCases:count,checks,status:'passed',scope:'Static design prototype only; not production compliance or API acceptance'};
  writeFileSync(path.join(output,`${options.mode}-result.json`),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
}finally{await browser?.close();if(server){server.kill('SIGTERM');}}
