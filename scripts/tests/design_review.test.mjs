import test from 'node:test';
import assert from 'node:assert/strict';
import {readdir,readFile} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
import {themes,pages,states,previewURL,escapeHTML,getTheme,getPage} from '../../docs/design-review-2026-10/data.mjs';
import {emptyReview,validateReview,mergeReviews,noteKey} from '../../docs/design-review-2026-10/review-store.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url));
async function routes(directory,relative='') {
  const found=[];
  for(const entry of await readdir(path.join(directory,relative),{withFileTypes:true})){
    const name=path.join(relative,entry.name);
    if(entry.isDirectory())found.push(...await routes(directory,name));
    else if(entry.name==='page.tsx')found.push('/'+relative.split(path.sep).filter(Boolean).join('/'));
  }
  return found.sort();
}
const luminance=hex=>{const c=hex.slice(1).match(/../g).map(c=>parseInt(c,16)/255).map(c=>c<=.04045?c/12.92:((c+.055)/1.055)**2.4);return c[0]*.2126+c[1]*.7152+c[2]*.0722;};
const contrast=(a,b)=>{const x=luminance(a),y=luminance(b);return (Math.max(x,y)+.05)/(Math.min(x,y)+.05);};

test('design inventory covers every existing public and operations page exactly',async()=>{
  for(const [surface,app] of [['public','display-web'],['ops','ops-web']])assert.deepEqual(pages[surface].filter(p=>!p.proposed).map(p=>p.route).sort(),await routes(path.join(root,'apps',app,'app')));
  assert.equal(pages.public.filter(p=>!p.proposed).length,25);assert.equal(pages.ops.length,26);assert.equal(pages.public.filter(p=>p.proposed).length,3);
  for(const list of Object.values(pages)){assert.equal(new Set(list.map(p=>p.id)).size,list.length);assert.equal(new Set(list.map(p=>p.route)).size,list.length);}
});
test('20 distinct layout signatures and readable base text tokens',()=>{
  assert.equal(themes.length,20);assert.equal(new Set(themes.map(t=>t.layout)).size,20);assert.equal(new Set(themes.map(t=>t.id)).size,20);
  for(const t of themes){
    assert.equal(t.colors.length,6);
    for(const color of t.colors)assert.match(color,/^#[0-9a-f]{6}$/);
    for(const [fg,bg] of [[2,0],[2,1],[3,0],[3,1],[5,4]])assert.ok(contrast(t.colors[fg],t.colors[bg])>=4.5,`${t.id} ${t.name} ${fg}/${bg}: ${contrast(t.colors[fg],t.colors[bg])}`);
    assert.ok(contrast(t.colors[4],t.colors[0])>=3,`${t.id} focus indicator contrast`);
  }
});
test('preview URLs encode parameters and safe fallbacks remain in inventory',()=>{
  assert.equal(getTheme('bad').id,'01');assert.equal(getPage('public','bad').id,'home');
  assert.match(previewURL('01','public','work','ready'),/^preview\.html\?/);assert.equal(new URL(previewURL('01','public','"><script>'),'http://localhost').searchParams.get('page'),'">\x3cscript>');
  assert.equal(escapeHTML('<img src=x onerror="alert(1)">'), '&lt;img src=x onerror=&quot;alert(1)&quot;&gt;');assert.equal(Object.keys(states).length,5);
});
const note={theme:'01',surface:'public',page:'home',viewport:'desktop',state:'ready',rating:'shortlist',text:'Keep the layout'};
test('review exports validate, merge and retain independent page/viewport opinions',()=>{
  const first={...emptyReview(),favorites:['01'],notes:[note]};
  const next={...emptyReview(),favorites:['02'],notes:[{...note,text:'Updated'},{...note,viewport:'mobile',text:'Simplify mobile'}]};
  const merged=mergeReviews(first,next);assert.deepEqual(merged.favorites,['01','02']);assert.equal(merged.notes.length,2);assert.equal(merged.notes[0].text,'Updated');assert.notEqual(noteKey(merged.notes[0]),noteKey(merged.notes[1]));assert.equal(first.notes[0].text,'Keep the layout');
});
test('review import rejects malformed, oversized and unknown states without executing text',()=>{
  for(const value of [null,{}, {version:2,favorites:[],notes:[]},{...emptyReview(),favorites:['999']},{...emptyReview(),notes:[{...note,page:'missing'}]},{...emptyReview(),notes:[{...note,state:'__proto__'}]},{...emptyReview(),notes:[{...note,text:'x'.repeat(4001)}]},{...emptyReview(),notes:[note,note]}])assert.throws(()=>validateReview(value));
  const safe=validateReview({...emptyReview(),notes:[{...note,text:'<script>alert(1)</script>',unexpected:'discarded'}]});assert.equal(safe.notes[0].text,'<script>alert(1)</script>');assert.equal(safe.notes[0].unexpected,undefined);
});
test('prototype renderer covers every page kind and has no third-party includes',async()=>{
  const source=await readFile(path.join(root,'docs/design-review-2026-10/preview.mjs'),'utf8');
  for(const p of [...pages.public,...pages.ops])assert.ok(source.includes(p.kind),p.kind);
  for(const file of ['index.html','preview.html','review.css','preview.css']){
    const content=await readFile(path.join(root,'docs/design-review-2026-10',file),'utf8');
    assert.doesNotMatch(content,/<(?:script|img|link)[^>]+(?:src|href)=["']https?:/i);
    assert.doesNotMatch(content,/@import\s|url\(["']?https?:/i);
  }
});
