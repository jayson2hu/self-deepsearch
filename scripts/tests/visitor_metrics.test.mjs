import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {webcrypto} from 'node:crypto';
import vm from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

function harness(){
  const store=new Map(),exports={};
  const context={exports,crypto:webcrypto,Uint8Array,Date,Event,window:{dispatchEvent(){}},navigator:{},document:{querySelector:()=>({getAttribute:()=> 'enabled'})},localStorage:{getItem:k=>store.get(k)??null,setItem:(k,v)=>store.set(k,v),removeItem:k=>store.delete(k)}};
  const code=ts.transpileModule(readFileSync('apps/display-web/lib/visitor-metrics.ts','utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText;
  vm.runInNewContext(code,context);return{store,api:exports,context};
}
test('visitor ID requires consent, persists, rotates, and stops on deny',()=>{
  const {store,api}=harness();assert.equal(api.getVisitorToken(),undefined);assert.equal(store.size,0);
  assert.equal(api.setVisitorChoice('allow'),true);assert.equal(store.has('mujian_visitor_v1'),false);
  const first=api.getVisitorToken();assert.match(first,/^[a-f0-9-]{36}$/);assert.equal(api.getVisitorToken(),first);
  store.set('mujian_visitor_v1',JSON.stringify({token:first,expiresAt:Date.now()-1}));assert.notEqual(api.getVisitorToken(),first);
  api.setVisitorChoice('deny');assert.equal(api.getVisitorToken(),undefined);assert.equal(store.has('mujian_visitor_v1'),false);
});
test('GPC, DNT, environment disable and storage failures never create an ID',()=>{
  for(const signal of ['gpc','dnt','disabled','storage']){
    const {store,api,context}=harness();api.setVisitorChoice('allow');
    if(signal==='gpc')context.navigator.globalPrivacyControl=true;
    if(signal==='dnt')context.navigator.doNotTrack='1';
    if(signal==='disabled')context.document.querySelector=()=>null;
    if(signal==='storage')context.localStorage.getItem=()=>{throw new Error('blocked')};
    assert.equal(api.getVisitorToken(),undefined);assert.equal(store.has('mujian_visitor_v1'),false);
  }
});
