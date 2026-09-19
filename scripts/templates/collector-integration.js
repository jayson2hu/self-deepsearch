const $ = (selector) => document.querySelector(selector);
let page = 1, requestVersion = 0;
const node = (tag, text, className) => { const el = document.createElement(tag); if (text !== undefined) el.textContent = text; if (className) el.className = className; return el; };
const aliases = item => Array.isArray(item.aliases) ? item.aliases.filter(value => typeof value === 'string' && value.trim()) : [];
async function json(url) {
  const response = await fetch(url, {credentials:'same-origin', cache:'no-store'});
  if (!response.ok) throw new Error(response.status === 401 ? '登录已失效，请在同一主机的 13001 后台重新登录。' : '读取失败，请重试。');
  return response.json();
}
function portrait(item) {
  const allowed = item.avatar && item.media_review_status !== 'rejected' && !['restricted','takedown'].includes(item.media_rights_status);
  if (!allowed) return node('div', item.avatar ? '头像待处理' : '暂无头像', 'placeholder');
  const image = node('img'); image.src = `/media/${item.avatar.media_candidate_id}.jpg`; image.alt = `${item.name}的待确认头像`;
  image.width = 125; image.height = 125;
  image.addEventListener('error', () => image.replaceWith(node('div', '头像不可用', 'placeholder')));
  return image;
}
async function detail(id) {
  try {
    const {items} = await json(`/api/performers/${encodeURIComponent(id)}`);
    const item = items[0]; $('#detail h2').textContent = item.name;
    const list = node('dl');
    list.append(node('dt','已收录别名 / 曾用名'),node('dd',aliases(item).join('、') || '来源未提供'));
    for (const [label,value] of [['稳定人物 ID',item.performer_id],['来源 ID',item.external_id],['状态',`${item.rights_status} / ${item.publication_status}`],['候选状态',item.candidate_status],['来源检查时间（非公开更新时间）',item.checked_at],['内容 SHA-256',item.current_content_hash],['头像 SHA-256',item.avatar?.sha256 ?? '未取得头像'],['图片尺寸',item.avatar ? `${item.avatar.width} × ${item.avatar.height}` : '无']]) list.append(node('dt',label),node('dd',value));
    $('#detail-body').replaceChildren(portrait(item),list); $('#detail').showModal();
  } catch(error) { $('#error').hidden = false; $('#error').textContent = error.message; }
}
async function load() {
  const version = ++requestVersion; $('#error').hidden = true; $('#result').textContent = '读取测试库中…';
  const params = new URLSearchParams({q:$('#query').value,page:String(page),avatar:$('#avatar').value});
  try {
    const data = await json(`/api/performers?${params}`);
    if (version !== requestVersion) return;
    $('#counts').replaceChildren(...[['人物候选',data.counts.performers],['本地头像',data.counts.avatars],['暂无头像',data.counts.without_avatar]].map(([label,value]) => {const box=node('div');box.append(node('strong',value.toLocaleString('en-US')),node('span',label));return box;}));
    $('#cards').replaceChildren(...data.items.map(item => {
      const card = node('article',undefined,'card'); card.dataset.id=item.performer_id;
      const button = node('button','查看详情'); button.addEventListener('click',()=>detail(item.performer_id));
      card.append(portrait(item),node('h2',item.name),node('code',item.external_id));
      if(aliases(item).length)card.append(node('p',`别名：${aliases(item).join('、')}`));
      card.append(node('p','待确认 · 未公开','state'),button);return card;
    }));
    const pages = Math.max(1,Math.ceil(data.total/data.page_size));
    $('#result').textContent = data.total ? `共 ${data.total} 条 · 每页 ${data.page_size} 条` : '没有匹配的资料';
    $('#page').textContent = `${page} / ${pages}`; $('#previous').disabled = page<=1; $('#next').disabled = page>=pages;
  } catch(error) {if(version!==requestVersion)return;$('#cards').replaceChildren();$('#result').textContent='';$('#error').hidden=false;$('#error').textContent=error.message;}
}
$('#search-form').addEventListener('submit',event=>{event.preventDefault();page=1;load();});
$('#previous').addEventListener('click',()=>{page--;load();});
$('#next').addEventListener('click',()=>{page++;load();});
$('#close').addEventListener('click',()=>$('#detail').close());
load();
