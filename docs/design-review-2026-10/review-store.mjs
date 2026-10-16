import {themes, pages, states} from './data.mjs';

export const STORAGE_KEY = 'self-deepsearch.design-review.2026-10.v1';
export const ratings = {pending:'待审核', shortlist:'进入候选', revise:'需要修改', reject:'不采用'};
export const noteKey = note => [note.theme,note.surface,note.page,note.viewport,note.state].join(':');
export const emptyReview = () => ({version:1,updatedAt:null,favorites:[],notes:[]});

// Import is data, never markup or executable state. Rebuild every allowed field.
export function validateReview(value) {
  if (!value || typeof value !== 'object' || value.version !== 1 || !Array.isArray(value.favorites) || !Array.isArray(value.notes)) throw new Error('不是支持的审核文件（需要 version: 1）。');
  if (value.favorites.length > 20 || value.notes.length > 6000) throw new Error('审核文件条目过多。');
  const validTheme = id => typeof id === 'string' && themes.some(t=>t.id===id);
  if (!value.favorites.every(validTheme)) throw new Error('收藏包含未知方案。');
  const keys = new Set();
  const notes = value.notes.map(note=>{
    if (!note || !validTheme(note.theme) || !['public','ops'].includes(note.surface) || !pages[note.surface].some(p=>p.id===note.page) || !['desktop','mobile'].includes(note.viewport) || !Object.hasOwn(states,note.state) || !Object.hasOwn(ratings,note.rating) || typeof note.text!=='string' || note.text.length>4000) throw new Error('审核意见包含未知页面、状态或超长文字。');
    const clean={theme:note.theme,surface:note.surface,page:note.page,viewport:note.viewport,state:note.state,rating:note.rating,text:note.text};
    const key=noteKey(clean);if(keys.has(key))throw new Error('审核文件包含重复意见。');keys.add(key);return clean;
  });
  return {version:1,updatedAt:typeof value.updatedAt==='string'?value.updatedAt.slice(0,40):null,favorites:[...new Set(value.favorites)],notes};
}
export function mergeReviews(current, incoming) {
  const a=validateReview(current),b=validateReview(incoming);
  const notes=new Map(a.notes.map(note=>[noteKey(note),note]));
  for(const note of b.notes) notes.set(noteKey(note),note);
  return validateReview({version:1,updatedAt:new Date().toISOString(),favorites:[...new Set([...a.favorites,...b.favorites])],notes:[...notes.values()]});
}
