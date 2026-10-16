import {themes,pages,states,getTheme,getPage,previewURL,escapeHTML as esc} from './data.mjs';
import {STORAGE_KEY,ratings,emptyReview,validateReview,mergeReviews,noteKey} from './review-store.mjs';

const $ = selector=>document.querySelector(selector);
const params=new URLSearchParams(location.search);
const selection={theme:getTheme(params.get('theme')).id,surface:params.get('surface')==='ops'?'ops':'public',page:'home',viewport:params.get('viewport')==='mobile'?'mobile':'desktop',state:Object.hasOwn(states,params.get('state'))?params.get('state'):'ready',compare:false,second:'02'};
selection.page=getPage(selection.surface,params.get('page')).id;
let view=params.get('view')==='review'?'review':'gallery';
let family='全部',review=emptyReview(),storageAvailable=true;
try {const stored=localStorage.getItem(STORAGE_KEY);if(stored)review=validateReview(JSON.parse(stored));}catch {storageAvailable=false;}
const notify = message=>{const el=$('#lab-message');el.textContent=message;el.classList.add('visible');clearTimeout(notify.timer);notify.timer=setTimeout(()=>el.classList.remove('visible'),6000);};
function persist() {
  review.updatedAt=new Date().toISOString();
  try {localStorage.setItem(STORAGE_KEY,JSON.stringify(review));storageAvailable=true;}catch {storageAvailable=false;}
  $('#save-status').textContent=storageAvailable?'已保存在此浏览器。导出后可备份或转交。':'浏览器存储不可用；意见暂存在内存，请立即导出备份。';
}
function setView(next) {
  view=next;
  for(const name of ['gallery','review','launch']){$(`#${name}-view`).hidden=name!==next;$(`#${name}-tab`).setAttribute('aria-pressed',String(name===next));}
  if(next==='review')renderReview();if(next==='gallery')renderGallery();
  updateURL();
}
function updateURL() {const q=new URLSearchParams({view,theme:selection.theme,surface:selection.surface,page:selection.page,viewport:selection.viewport,state:selection.state});history.replaceState(null,'',`${location.pathname}?${q}`);}
function toggleFavorite(id) {review.favorites=review.favorites.includes(id)?review.favorites.filter(x=>x!==id):[...review.favorites,id];persist();renderGallery();updateFavoriteButton();}
function renderGallery() {
  $('#favorite-count').textContent=review.favorites.length;
  const mode=$('#gallery-mode').value;
  const screenshotLabel={public:'公开站桌面',ops:'运营后台桌面',mobile:'公开站手机'}[mode];
  const filtered=themes.filter(t=>(family==='全部'||t.family===family)&&(!$('#favorites-only').checked||review.favorites.includes(t.id)));
  $('#theme-gallery').innerHTML=filtered.map(t=>`<article class="theme-card"><div class="theme-image ${mode==='mobile'?'is-mobile':''}"><button class="image-button" data-review="${t.id}" aria-label="审核 ${t.id} ${t.name}"><img src="thumbs/${t.id}-${mode}.jpg" alt="${esc(t.english)} ${screenshotLabel}设计截图" width="${mode==='mobile'?390:1440}" height="${mode==='mobile'?844:1000}" loading="lazy"><span class="image-number">${t.id}</span></button><button class="favorite-star" data-favorite="${t.id}" aria-label="收藏 ${t.name}" aria-pressed="${review.favorites.includes(t.id)}">${review.favorites.includes(t.id)?'★':'☆'}</button></div><div class="theme-card-content"><div class="theme-card-title"><h2>${t.name}</h2><span>${t.family}</span></div><span class="english-name">${t.english}</span><p>${t.description}</p><div class="theme-card-bottom"><span class="swatches" aria-label="方案配色">${[t.colors[0],t.colors[2],t.colors[4]].map(c=>`<i style="background:${c}"></i>`).join('')}</span><button data-review="${t.id}">进入审核 <span aria-hidden="true">↗</span></button></div></div></article>`).join('');
  $('#gallery-empty').hidden=filtered.length>0;
  $('#family-filters').innerHTML=['全部',...new Set(themes.map(t=>t.family))].map(name=>`<button data-family="${name}" aria-pressed="${name===family}">${name}</button>`).join('');
}
const themeOptions = ()=>themes.map(t=>`<option value="${t.id}">${t.id} · ${t.name}</option>`).join('');
$('#theme-select').innerHTML=themeOptions();$('#compare-select').innerHTML=themeOptions();
$('#state-select').innerHTML=Object.entries(states).map(([key,label])=>`<option value="${key}">${label}</option>`).join('');
$('#rating-select').innerHTML=Object.entries(ratings).map(([key,label])=>`<option value="${key}">${label}</option>`).join('');
function renderPageOptions() {
  const groups=[...new Set(pages[selection.surface].map(p=>p.group))];
  $('#page-select').innerHTML=groups.map(group=>`<optgroup label="${group}">${pages[selection.surface].filter(p=>p.group===group).map(p=>`<option value="${p.id}">${esc(p.title)}${p.proposed?' [新增提案]':''}</option>`).join('')}</optgroup>`).join('');
  $('#page-select').value=selection.page;
}
function noteContext() {return {theme:selection.theme,surface:selection.surface,page:selection.page,viewport:selection.viewport,state:selection.state};}
function loadNote() {
  const note=review.notes.find(note=>noteKey(note)===noteKey(selection));
  $('#note-text').value=note?.text||'';$('#rating-select').value=note?.rating||'pending';
  $('#note-context').textContent=`${selection.theme} / ${selection.surface==='public'?'公开站':'运营'} / ${selection.viewport==='desktop'?'桌面':'手机'} / ${states[selection.state]}`;
  $('#save-status').textContent=storageAvailable?'意见仅保存在此浏览器，建议导出备份。':'无法读取浏览器存储；请导出备份，避免刷新丢失。';
}
function saveNote() {
  const note={...noteContext(),rating:$('#rating-select').value,text:$('#note-text').value};
  const index=review.notes.findIndex(item=>noteKey(item)===noteKey(note));
  if(index>=0)review.notes.splice(index,1);
  if(note.text||note.rating!=='pending')review.notes.push(note);
  persist();
}
function updateFavoriteButton() {$('#favorite-current').textContent=review.favorites.includes(selection.theme)?'★ 已收藏方案':'☆ 收藏方案';$('#favorite-current').setAttribute('aria-pressed',String(review.favorites.includes(selection.theme)));}
function updateInspector() {
  const theme=getTheme(selection.theme),page=getPage(selection.surface,selection.page);
  $('#theme-title').textContent=`${theme.id} · ${theme.name}`;
  for(const name of ['theme','surface','viewport','state'])$(`#${name}-select`).value=selection[name];
  $('#compare-toggle').checked=selection.compare;$('#compare-control').hidden=!selection.compare;$('#compare-select').value=selection.second;
  $('#route-label').textContent=`${page.route}${page.proposed?' · 新增页面提案':''}`;
  $('#open-preview').href=previewURL(selection.theme,selection.surface,selection.page,selection.state);
  $('#canvas-description').textContent=theme.description;
  updateFavoriteButton();loadNote();updateURL();
}
const observer=new ResizeObserver(entries=>entries.forEach(({target})=>sizeFrame(target)));
function sizeFrame(windowEl) {
  const viewport=selection.viewport==='mobile'?390:1440,height=selection.viewport==='mobile'?844:1000;
  const width=windowEl.clientWidth;
  const scale=Math.min(1,width/viewport);
  const frame=windowEl.querySelector('iframe');if(!frame)return;
  frame.style.width=`${viewport}px`;frame.style.height=`${height}px`;frame.style.transform=`scale(${scale})`;
  windowEl.style.height=`${height*scale}px`;
  windowEl.style.maxWidth=`${viewport}px`;
}
function renderPanes() {
  observer.disconnect();
  const ids=selection.compare?[selection.theme,selection.second]:[selection.theme];
  $('#preview-panes').classList.toggle('compare',selection.compare);
  $('#preview-panes').innerHTML=ids.map((id,i)=>`<section class="preview-pane"><div class="pane-label"><strong>${id} · ${getTheme(id).name}</strong><span>${i===0?'主方案（意见记录在此）':'对比方案'} / ${selection.viewport==='mobile'?'390 × 844':'1440 × 1000'}</span></div><div class="frame-window ${selection.viewport}" data-pane="${i}"><iframe title="${id} ${getTheme(id).name} ${selection.surface==='public'?'公开站':'运营后台'} ${getPage(selection.surface,selection.page).title}" src="${previewURL(id,selection.surface,selection.page,selection.state)}" sandbox="allow-scripts allow-same-origin allow-forms allow-downloads"></iframe></div><a class="pane-open" href="${previewURL(id,selection.surface,selection.page,selection.state)}" target="_blank" rel="noopener">原尺寸独立查看 ${id} ↗</a></section>`).join('');
  document.querySelectorAll('.frame-window').forEach(el=>{sizeFrame(el);observer.observe(el);});
}
function renderReview() {renderPageOptions();updateInspector();renderPanes();}
document.addEventListener('click',event=>{
  const el=event.target.closest('button');if(!el)return;
  if(el.dataset.review) {selection.theme=el.dataset.review;selection.surface=$('#gallery-mode').value==='ops'?'ops':'public';selection.viewport=$('#gallery-mode').value==='mobile'?'mobile':'desktop';selection.page='home';selection.state='ready';setView('review');window.scrollTo({top:0});}
  if(el.dataset.favorite)toggleFavorite(el.dataset.favorite);
  if(el.dataset.family){family=el.dataset.family;renderGallery();}
  if(el.dataset.launchPage){selection.surface='public';selection.page=el.dataset.launchPage;selection.state='ready';setView('review');window.scrollTo({top:0});}
});
for(const name of ['gallery','review','launch'])$(`#${name}-tab`).addEventListener('click',()=>setView(name));
$('#begin-review').addEventListener('click',()=>{setView('review');window.scrollTo({top:0});});
$('#favorites-only').addEventListener('change',renderGallery);
$('#gallery-mode').addEventListener('change',renderGallery);
$('#favorite-current').addEventListener('click',()=>toggleFavorite(selection.theme));
for(const name of ['theme','surface','page','viewport','state'])$(`#${name}-select`).addEventListener('change',event=>{selection[name]=event.target.value;if(name==='surface')selection.page='home';renderReview();});
$('#compare-toggle').addEventListener('change',event=>{selection.compare=event.target.checked;if(selection.second===selection.theme)selection.second=themes.find(t=>t.id!==selection.theme).id;updateInspector();renderPanes();});
$('#compare-select').addEventListener('change',event=>{selection.second=event.target.value;renderPanes();});
$('#note-text').addEventListener('input',saveNote);$('#rating-select').addEventListener('change',saveNote);
$('#export-review').addEventListener('click',()=>{const blob=new Blob([JSON.stringify(review,null,2)],{type:'application/json'});const url=URL.createObjectURL(blob);const a=document.createElement('a');a.href=url;a.download=`self-deepsearch-review-${new Date().toISOString().slice(0,10)}.json`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);notify(`已导出 ${review.favorites.length} 个收藏、${review.notes.length} 条意见。`);});
$('#import-review').addEventListener('click',()=>$('#import-file').click());
$('#import-file').addEventListener('change',async event=>{const file=event.target.files?.[0];if(!file)return;try{if(file.size>2*1024*1024)throw new Error('文件超过 2 MB，请检查是否选错文件。');const incoming=validateReview(JSON.parse(await file.text()));review=mergeReviews(review,incoming);persist();loadNote();updateFavoriteButton();renderGallery();notify(`已合并 ${incoming.notes.length} 条意见。`);}catch(error){notify(`导入失败：${error.message}`);}finally{event.target.value='';}});
window.addEventListener('message',event=>{
  if(event.origin!==location.origin||event.data?.type!=='deepsearch-preview')return;
  const frame=$('.frame-window[data-pane="0"] iframe');if(event.source!==frame?.contentWindow)return;
  const data=event.data;
  if(data.theme!==selection.theme||!['public','ops'].includes(data.surface)||!pages[data.surface].some(p=>p.id===data.page)||!Object.hasOwn(states,data.state))return;
  if(data.surface===selection.surface&&data.page===selection.page&&data.state===selection.state)return;
  selection.surface=data.surface;selection.page=data.page;selection.state=data.state;renderPageOptions();updateInspector();
  if(selection.compare){const second=$('.frame-window[data-pane="1"] iframe');if(second)second.src=previewURL(selection.second,selection.surface,selection.page,selection.state);}
  document.querySelectorAll('.pane-open').forEach((a,i)=>{a.href=previewURL(i?selection.second:selection.theme,selection.surface,selection.page,selection.state);});
});
renderGallery();setView(view);
