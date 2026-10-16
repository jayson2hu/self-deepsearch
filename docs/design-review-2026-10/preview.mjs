import {themes, pages, works, people, studios, states, getTheme, getPage, previewURL, escapeHTML as esc} from './data.mjs';

const query = new URLSearchParams(location.search);
const t = getTheme(query.get('theme'));
const surface = query.get('surface') === 'ops' ? 'ops' : 'public';
const p = getPage(surface, query.get('page'));
const state = Object.hasOwn(states, query.get('state')) ? query.get('state') : 'ready';
const root = document.documentElement;
['bg','panel','ink','muted','accent','on-accent'].forEach((key, i) => root.style.setProperty(`--${key}`, t.colors[i]));
root.style.setProperty('--display', `"${t.display}", ${/Georgia|Palatino|Times/.test(t.display) ? 'serif' : 'sans-serif'}`);
root.style.setProperty('--body', `"${t.body}", sans-serif`);
root.style.setProperty('--radius', `${t.radius}px`);
root.style.setProperty('--columns', t.columns);
document.body.className = `surface-${surface} layout-${t.layout} ops-${t.ops}`;
document.body.dataset.theme = t.id;
document.body.dataset.page = p.id;
document.body.dataset.state = state;
document.title = `${p.title} · ${t.english} · Design only`;

const href = (id, target = surface) => previewURL(t.id, target, id);
const link = (id, text, cls = '', target = surface) => `<a class="${cls}" href="${href(id, target)}" ${id === p.id && target === surface ? 'aria-current="page"' : ''}>${esc(text)}</a>`;
const button = (text, action = 'demo', cls = '') => `<button class="${cls}" data-action="${action}">${esc(text)}</button>`;
const badge = (text, cls = '') => `<span class="badge ${cls}">${esc(text)}</span>`;
const icon = (name) => {
  const paths = {search:'<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>', grid:'<rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/>', shield:'<path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6Z"/><path d="m8 12 3 3 5-6"/>', bookmark:'<path d="M6 3h12v18l-6-4-6 4Z"/>', arrow:'<path d="M4 12h16m-6-6 6 6-6 6"/>', plus:'<path d="M12 4v16M4 12h16"/>'};
  return `<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true">${paths[name] || paths.grid}</svg>`;
};

function poster(work, index = 0, extra = '') {
  return `<div class="poster art-${work.color} shape-${work.shape} ${extra}" role="img" aria-label="Abstract design placeholder for ${esc(work.title)}; not an actual cover">
    <span class="poster-imprint">Self Deepsearch archive</span><span class="art-shape" aria-hidden="true"></span>
    <span class="poster-type">${esc(work.title)}</span><span class="poster-footer">Design placeholder <span>${String(index + 1).padStart(2, '0')}</span></span>
  </div>`;
}
function searchBox(large = false) {
  return `<form class="search-form ${large ? 'large-search' : ''}" data-form="search" role="search">
    <label class="sr-only" for="archive-search">Search the archive</label>${icon('search')}
    <input id="archive-search" name="query" type="search" placeholder="Title, performer or catalogue no." maxlength="100" autocomplete="off">
    <button class="primary" type="submit">Search</button></form>`;
}
function workCards(count = 6, list = works) {
  return `<div class="work-grid">${list.slice(0, count).map((w, i) => `<article class="work-card">
    <a class="cover-link" href="${href('work','public')}">${poster(w, i)}</a>
    <div class="work-caption"><div><a class="work-title" href="${href('work','public')}">${esc(w.title)}</a><p>${esc(w.studio)} <span class="meta-year">${w.year}</span></p></div>
    <button class="save-icon" data-action="save" aria-label="Save ${esc(w.title)}" aria-pressed="false">${icon('bookmark')}</button></div>
    <span class="catalogue-code">${w.code} · Fictional record</span>
  </article>`).join('')}</div>`;
}
function header() {
  const brand = `<a class="brand" href="${href('home')}"><span class="brand-mark" aria-hidden="true">sd<span>.</span></span><span>Self<br>Deepsearch</span></a>`;
  if (surface === 'ops') return `<div class="ops-shell"><aside class="ops-navigation">${brand}<span class="workspace-tag">Review workspace</span>
    <details class="mobile-menu"><summary>Workspace navigation</summary>${allNav()}</details><nav class="desktop-ops-nav" aria-label="Workspace">${allNav()}</nav>
    <div class="operator"><span class="initials">DR</span><span>Demo reviewer<br><small>Restricted access</small></span></div></aside><div class="ops-body">
    <header class="ops-top"><span>${esc(p.group)} / ${esc(p.title)}</span><div>${badge('Test environment')}${button('Help')}</div></header>`;
  const mainLinks = [['home','Discover'],['latest','New releases'],['performers','Performers'],['studios','Studios']];
  return `<header class="public-header">${brand}<nav class="desktop-nav" aria-label="Primary">${mainLinks.map(([id,label])=>link(id,label)).join('')}</nav>
    <div class="header-tools">${link('search', 'Search', 'search-link')}${link('account','My library','library-link')}<span class="age-chip">18+</span></div>
    <details class="mobile-menu"><summary>Menu</summary><nav aria-label="Mobile">${mainLinks.map(([id,label])=>link(id,label)).join('')}${link('search','Search')}${link('account','My library')}${link('report','Report a concern')}</nav></details></header>`;
}
function allNav() {
  const groups = [...new Set(pages.ops.map(item => item.group))];
  return groups.map(group => `<div class="nav-group"><span>${esc(group)}</span>${pages.ops.filter(item=>item.group===group && !['login','logout'].includes(item.id)).map(item=>link(item.id,item.title)).join('')}</div>`).join('');
}
function footer() {
  return `<footer class="site-footer"><div><strong>Self Deepsearch</strong><p>For adults. Metadata only.<br>No streaming, downloads or explicit imagery in this prototype.</p></div><nav aria-label="Trust and safety">
    ${link('about','About','','public')}${link('privacy','Privacy notice','','public')}${link('age','Age & access','','public')}${link('report','Report a concern','','public')}${link('cookies','Privacy choices','','public')}
    </nav><span>Fictional data<br>Design review · 2026</span></footer>`;
}
function heading(actions = '') {
  return `<div class="page-heading"><div><p class="breadcrumb">${surface === 'ops' ? 'Workspace' : p.group} / ${esc(p.title)}</p><h1>${esc(p.title)}</h1><p class="intro">${esc(p.description)}</p></div>${actions ? `<div class="actions">${actions}</div>` : ''}</div>`;
}
function trustLine() {return `<div class="trust-line">${icon('shield')}<span>Adult audience · Published metadata only · Rights & privacy matter</span>${link('report','Report a concern')}</div>`;}
function sectionTitle(title, more = 'latest') {return `<div class="section-heading"><h2>${esc(title)}</h2>${link(more,'View all','text-link')}</div>`;}

function home() {
  const intro = `<div class="hero-copy"><span class="edition">An independent adult metadata archive</span><h1>Find a work.<br>Follow a name.<br>Keep a collection.</h1><p>A considered place to explore works, performers and the studios behind them. Metadata, without the noise.</p>
    <div class="actions">${link('search','Explore the archive','primary')}${link('latest','See new releases','secondary')}</div><span class="hero-note">18+ audience. All records shown are fictional.</span></div>`;
  const featured = `<a class="hero-art" href="${href('work')}">${poster(works[0],0,'hero-poster')}<div class="hero-art-caption"><span>Featured record</span><strong>After the rain</strong><span>Northlight · SD-026 ${icon('arrow')}</span></div></a>`;
  const collection = `<aside class="hero-index"><h2>Browse the index</h2>${[['works','Works','search'],['people','Performers','performers'],['studios','Studios','studios']].map(([type,label,id])=>`<a href="${href(id)}"><span>${esc(label)}</span>${icon('arrow')}</a>`).join('')}<p>A structured catalogue.<br>A private collection.<br>Your own pace.</p></aside>`;
  let hero;
  if (['console','atlas','library'].includes(t.layout)) {
    hero = `<section class="hero search-hero"><div><span class="edition">Search the adult metadata index</span><h1>${t.layout === 'atlas' ? 'The archive,<br>field by field.' : 'A catalogue.<br>Not an algorithm.'}</h1><p>Find a title, follow a credit, or trace a studio’s catalogue.</p>${searchBox(true)}<div class="suggestions">Try a fictional record: ${link('work','SD-026')} ${link('performer','Alex Morgan')}</div></div>${collection}</section>`;
  } else if (t.layout === 'open') {
    hero = `<section class="hero open-hero">${intro}<div class="hero-shelf">${works.slice(0,3).map((w,i)=>`<a href="${href('work')}">${poster(w,i)}</a>`).join('')}<p>A shelf of possibilities. Your collection starts here.</p></div></section>${searchBox(true)}`;
  } else if (t.layout === 'news') {
    hero = `<div class="news-masthead"><span>Independent metadata index</span><h1>The Deepsearch Record</h1><span>Review edition · October 2026</span></div><section class="hero news-hero">${featured}<div class="hero-copy"><h2>New names.<br>New perspectives.</h2><p>Browse the latest additions to an independent adult metadata archive.</p>${link('recent','Read the latest additions','primary')}</div>${collection}</section>`;
  } else if (t.layout === 'channel') {
    hero = `<section class="hero channel-hero">${intro}<div class="double-cover">${featured}<a href="${href('work')}">${poster(works[2],2)}</a></div></section>`;
  } else if (['marquee','essential'].includes(t.layout)) {
    hero = `<section class="hero centered-hero">${intro}${t.layout === 'marquee' ? `<div class="marquee-art">${featured}</div>` : searchBox(true)}</section>`;
  } else {
    hero = `<section class="hero">${['archive','shelf'].includes(t.layout) ? collection : ''}${intro}${featured}</section>`;
  }
  return `${hero}${trustLine()}<section class="collection-section">${sectionTitle(['archive','library','atlas'].includes(t.layout) ? 'Catalogue highlights' : 'Worth a closer look')}<div class="filter-tabs" role="group" aria-label="Browse catalogue">${['All works','New releases','Recently indexed'].map((s,i)=>`<button data-action="filter" aria-pressed="${i===0}">${s}</button>`).join('')}</div>${workCards(t.columns === 2 ? 4 : 6)}</section>
    <section class="editorial-strip"><div><h2>Follow the people.<br>Discover the connections.</h2><p>Browse fictional profiles and credited works.</p></div>${link('performers','Explore performers','primary')}</section>`;
}
function filterPanel() {
  return `<details class="filters" open><summary>Filter catalogue</summary><div class="filter-fields"><label>Studio<select data-filter="studio" aria-label="Studio"><option value="">All studios</option>${studios.map(s=>`<option>${s}</option>`).join('')}</select></label><label>Release year<select data-filter="year" aria-label="Release year"><option value="">Any year</option><option>2026</option><option>2025</option><option>2024</option></select></label><label>Sort by<select data-filter="sort" aria-label="Sort by"><option value="recent">Recently added</option><option value="title">Title A–Z</option></select></label>${button('Reset filters','reset-filters')}</div></details>`;
}
function worksPage() {return `${heading(p.group==='Account' ? button(p.id==='history'?'Clear demo history':'Manage list','delete') : '')}${searchBox()}${filterPanel()}<p id="result-count" role="status">8 fictional records</p><div id="results">${workCards(8)}</div>`;}
function peoplePage() {
  const list = p.kind === 'studios' ? studios : people;
  return `${heading()}<div class="alphabet" aria-label="Index shortcuts">${['All','A–F','G–M','N–S','T–Z'].map((s,i)=>`<button data-action="alphabet" aria-pressed="${i===0}">${s}</button>`).join('')}</div><div class="people-grid">${list.map((name,i)=>`<a class="person-card" data-name="${name}" href="${href(p.kind==='studios'?'studio':'performer')}"><div class="letter-portrait color-${i%3}" aria-hidden="true">${name.split(' ').map(x=>x[0]).join('').slice(0,2)}</div><h2>${name}</h2><p>Fictional ${p.kind==='studios'?'studio':'profile'} · ${12+i*3} sample credits</p><span>View catalogue ${icon('arrow')}</span></a>`).join('')}</div>`;
}
function detailPage() {
  const isWork = p.kind === 'work';
  return `${heading()}<section class="detail-layout"><div class="detail-visual">${isWork ? poster(works[0]) : `<div class="letter-portrait large-portrait">${p.kind==='studio'?'NS':'AM'}</div>`}<span class="image-note">${isWork?'Abstract cover':'Initials only'} · Design placeholder</span></div><div class="detail-copy">${badge('Synthetic record')}<h2>${isWork?'A record, in context.':'Credits and connections.'}</h2><p>This illustrative catalogue entry demonstrates how public metadata, sources and user actions fit together. It does not describe an actual film or person.</p><dl class="metadata"><div><dt>${isWork?'Catalogue number':'Profile type'}</dt><dd>${isWork?'SD-026':p.kind==='studio'?'Fictional studio':'Fictional performer'}</dd></div><div><dt>Studio</dt><dd>${link('studio','Northlight')}</dd></div><div><dt>Release date</dt><dd>2026-09-24 <small>(sample)</small></dd></div><div><dt>Source status</dt><dd>Design sample; not verified evidence</dd></div><div><dt>Media availability</dt><dd>Metadata only. No playback or download.</dd></div></dl><div class="actions">${button(isWork?'Save to library':'Follow profile','save','primary')}${button('Hide this record','delete')}</div><p class="fine-print">Private by default. ${link('report','Report incorrect or harmful content','inline-link')}</p></div></section><section>${sectionTitle(isWork?'More from this studio':'Catalogue credits')}${workCards(4)}</section>`;
}
function input(label, name, type='text', value='', required = false) {
  return `<label class="field">${esc(label)}<input name="${name}" type="${type}" value="${esc(value)}" ${required?'required':''} ${type==='password'?'minlength="12" autocomplete="new-password"':'autocomplete="off"'} maxlength="200"></label>`;
}
function authPage() {
  const register = p.id==='register', forgot = p.id==='forgot', invite = p.id==='invite';
  return `<div class="auth-layout"><section class="auth-story"><span class="edition">Your own corner of the archive</span><h2>Make space<br>for discovery.</h2>${poster(works[1],1)}<p>Fictional design preview.<br>Never enter a real password or identity document.</p></section><section class="auth-panel">${heading()}<form data-form="demo" class="stack-form">${invite?input('Invitation code','invitation','text','DEMO-INVITE',true):input('Email address','email','email','',true)}${!forgot&&!invite?input('Demo password (12+ characters)','password','password','',true):''}${register?`<label class="check"><input type="checkbox" required>I understand this is a design preview, not a real account.</label><p class="fine-print">Age eligibility must be checked independently where required.</p>`:''}<button class="primary">${forgot?'Preview reset request':invite?'Check demo invitation':register?'Preview account creation':'Preview sign in'}</button><p class="form-status" role="status"></p></form><div class="auth-links">${link('forgot','Forgot password?','',surface)}${surface==='public'?link(register?'login':'register',register?'Already have an account?':'Create an account'):link('home','Back to demo workspace')}</div>${link('privacy','Read privacy notice','inline-link','public')}</section></div>`;
}
function accountPage() {
  return `${heading(link('settings','Account settings','secondary'))}<div class="account-summary"><span class="initials">DR</span><div><h2>Demo reader</h2><p>Private library · Fictional account</p></div></div><div class="account-tiles">${[['favorites','Saved works','8'],['follows','Following','6'],['history','History','12'],['hidden','Hidden','2']].map(([id,label,n])=>link(id,`${label} · ${n}`,'account-tile')).join('')}</div>${sectionTitle('Your saved works','favorites')}${workCards(4)}<aside class="notice">Your library is personal. History retention and deletion behaviour require product approval before launch.</aside>`;
}
function settingsPage() {
  const fields = surface==='public' ? input('Display name','name','text','Demo reader')+input('Demo email','email','email','reader@example.invalid') : input('Workspace label','workspace','text','Self Deepsearch · Test')+input('Allowed file types','types','text','JPEG, PNG, WebP');
  return `${heading()}<div class="form-layout"><form class="panel stack-form" data-form="demo"><h2>${p.id==='upload-control'?'Upload safeguards':surface==='public'?'Profile details':'General settings'}</h2>${fields}<label class="field">Interface language<select><option>English</option><option>简体中文 (proposed)</option></select></label><button class="primary">Preview saved settings</button><p class="form-status" role="status"></p></form><section class="panel"><h2>Privacy & access</h2><p>${surface==='public'?'Request an export or review active sessions. No real personal data is stored in this preview.':'Quarantine unreviewed assets. Publishing requires evidence, authorization and an audit event.'}</p><div class="setting-row"><span>Optional analytics</span><label class="check"><input type="checkbox">Enabled in demo</label></div><div class="setting-row"><span>Required safeguards</span>${badge('Cannot be disabled here')}</div><div class="actions">${button('Preview export','demo')}${button('Revoke demo sessions','delete')}</div></section></div>`;
}
function feedbackPage() {
  return `${heading()}<div class="form-layout"><form class="panel stack-form" data-form="demo"><label class="field">Topic<select><option>Incorrect metadata</option><option>Accessibility</option><option>Account issue</option></select></label>${input('Record URL (optional)','record','url')}<label class="field">What needs attention?<textarea name="message" required maxlength="2000" rows="6"></textarea></label><button class="primary">Preview feedback receipt</button><p class="form-status" role="status"></p></form><aside class="panel"><h2>A safety concern?</h2><p>For ownership, consent or suspected underage content, use the dedicated reporting route. You should not need an account.</p>${link('report','Report a concern','secondary','public')}<p class="fine-print">Demo form: nothing is transmitted.</p></aside></div>`;
}
function policyPage() {
  const isPrivacy=p.id==='privacy';
  const sections=isPrivacy?[
    ['What this preview stores','Only design review preferences and notes in your browser. No login, browsing profile, identity documents or tracking SDKs are connected.'],
    ['What the live notice must explain','The legal controller, contact details, data purposes, lawful bases where applicable, vendors, retention periods and international transfers must match actual operations.'],
    ['Your choices','Optional tracking must remain off until the appropriate choice. Provide accessible export, correction, deletion and consent withdrawal routes.'],
    ['Age assurance and privacy','Design for proof of eligibility with minimal disclosure. Do not collect or retain raw ID documents in this prototype.'],
  ]:[
    ['An index, not a player','Works, performers and studios are connected through catalogue metadata. This concept does not provide streaming, audio or download links.'],
    ['Sources and corrections','Published records should explain their provenance. A correction and rights reporting route should be available from every detail page.'],
    ['Adults and eligibility','18+ audience labeling is not a substitute for legally required age assurance. Final access rules depend on content and jurisdiction.'],
    ['Before public launch','Confirm operator identity, intended countries, content rights, age verification, privacy policy and a staffed reporting process.'],
  ];
  return `${heading()}<div class="policy-layout"><nav aria-label="On this page">${sections.map(([title],i)=>`<a href="#policy-${i}">${title}</a>`).join('')}</nav><article class="prose"><div class="notice">Draft content for layout review. Not a final legal notice or a compliance certification.</div>${sections.map(([title,body],i)=>`<section id="policy-${i}"><h2>${title}</h2><p>${body}</p></section>`).join('')}${link('report','Contact and reporting','secondary','public')}</article></div>`;
}
function agePage() {
  return `${heading()}<section class="age-panel panel">${icon('shield')}<h2>Private proof of age.</h2><p>Restricted content stays unavailable until the required eligibility check succeeds. A self-declared birthday or “I am 18” button is not the implemented verification system.</p><div class="notice">Proposed flow only. No identity collection or verification provider is connected.</div><label class="field">Review a verification state<select id="age-state"><option value="pending">Awaiting verification</option><option value="unavailable">Provider unavailable</option><option value="restricted">Region restricted</option></select></label><div id="age-result" class="panel" role="status"><strong>Awaiting verification</strong><p>Choose an approved privacy-preserving method when a provider is integrated.</p></div><div class="actions">${button('Preview verification hand-off','age-demo','primary')}${link('about','Leave restricted area','secondary')}</div><p class="fine-print">No bypass or “verified” credential is created by this prototype.</p></section>`;
}
function reportPage() {
  return `${heading()}<div class="form-layout"><form class="panel stack-form" data-form="report"><div class="notice">Design demonstration only. This form does not send a report. Do not submit real sensitive information.</div><label class="field">Concern type<select name="reason"><option>Non-consensual content or image</option><option>Suspected underage content</option><option>Copyright or likeness rights</option><option>Incorrect metadata</option></select></label>${input('Affected page URL','url','url','',true)}${input('Contact email (optional in this concept)','email','email')}<label class="field">Brief description<textarea name="description" rows="5" required maxlength="2000"></textarea></label><p class="fine-print">Do not upload explicit evidence or identity documents. Final intake requirements depend on the applicable process.</p><button class="primary">Preview report receipt</button><p class="form-status" role="status"></p></form><aside class="panel"><h2>Safety comes first</h2><p>Access to reporting should not depend on registration, cookies or successfully completing an age check.</p><ol class="steps"><li>Receive a case reference</li><li>Triage safety and rights concerns</li><li>Restrict access when appropriate</li><li>Explain the outcome and appeal route</li></ol><p>Response deadlines must be configured for the target jurisdiction and applicable law. No live response time is promised here.</p></aside></div>`;
}
function cookiesPage() {
  return `${heading()}<section class="panel narrow"><h2>Your choices, clearly.</h2><p>This design preview has no advertising or analytics requests. The toggles below demonstrate the proposed experience.</p><div class="setting-row"><div><strong>Essential storage</strong><p>Required session and security functions in the live service.</p></div>${badge('Always active')}</div><label class="setting-row"><span><strong>Analytics</strong><span class="block-muted">Optional aggregate measurement</span></span><input type="checkbox" id="analytics-choice"></label><label class="setting-row"><span><strong>Personalisation</strong><span class="block-muted">Optional recommendation preferences</span></span><input type="checkbox" id="personalisation-choice"></label><div class="actions equal-actions">${button('Reject optional','reject-cookies','secondary')}${button('Save choices','save-cookies','secondary')}</div><p id="privacy-status" role="status">Optional features are off by default. Withdrawing consent must be as accessible as granting it.</p></section>`;
}

const tableConfigs = {
  catalog:{headers:['Record','Type','Status','Source','Action'],rows:works.slice(0,5).map((w,i)=>[w.title,'Work',i===1?'Needs review':'Published','Fictional source','Edit record']),target:'catalog-detail',action:'Create work',actionTarget:'new-work'},
  imports:{headers:['Batch','Validation','Records','Started','Action'],rows:[['Metadata October','Ready to review','24','Today, 09:32','Review batch'],['Studio corrections','3 errors','12','Today, 08:10','View errors'],['Archive September','Completed','180','Yesterday','View results']],target:'candidate',action:'Preview import'},
  collector:{headers:['Candidate','Provider','Review state','Received','Action'],rows:works.slice(0,5).map((w,i)=>[w.title,'Demo provider',i===1?'Conflicting fields':'Awaiting review','Today','Review candidate']),target:'candidate',action:'Run demo collection'},
  'media-usage':{headers:['Asset','Used by','Placement','Rights','Action'],rows:works.slice(0,4).map(w=>[`${w.code} abstract cover`,w.title,'Work cover','Demo only','Review usage']),target:'media'},
  users:{headers:['User','Role','Status','Last active','Action'],rows:[['reader@example.invalid','Reader','Active','Today','Review access'],['editor@example.invalid','Editor','Active','Yesterday','Review access'],['reviewer@example.invalid','Reviewer','Suspended','2 days ago','Review access']],action:'Review permissions'},
  feedback:{headers:['Case','Topic','Priority','Status','Action'],rows:[['FB-024','Incorrect release date','Normal','Open','Review case'],['FB-023','Keyboard focus issue','High','Assigned','Review case'],['FB-022','Studio naming','Normal','Resolved','Review case']]},
  audit:{headers:['Event','Actor','Object','Time','Action'],rows:[['Metadata updated','Demo editor','SD-026','09:32 UTC','View redacted diff'],['Publication requested','Demo reviewer','SD-025','09:15 UTC','View redacted diff'],['Asset quarantined','Demo operator','Asset 024','08:50 UTC','View redacted diff']]},
  'seo-issues':{headers:['Issue','Affected page','Severity','State','Action'],rows:[['Missing description','/works/demo-a','Warning','Open','Inspect record'],['Canonical mismatch','/studios/demo-b','High','Open','Inspect record'],['Private route check','/account','Info','Passed','Inspect record']],target:'catalog-detail'},
  'seo-reports':{headers:['Report','Scope','Result','Completed','Action'],rows:[['Indexability scan','Published catalogue','2 issues','Today','Open report'],['Metadata coverage','Work pages','96% sample','Yesterday','Open report'],['Private route exclusion','Account pages','Passed','Yesterday','Open report']],target:'seo-issues'},
};
function dataTable(config) {
  return `<div class="table-wrap"><table><caption class="sr-only">${esc(p.title)} · synthetic demonstration data</caption><thead><tr>${config.headers.map(h=>`<th scope="col">${h}</th>`).join('')}</tr></thead><tbody>${config.rows.map((row,i)=>`<tr>${row.map((cell,j)=>`<td data-label="${esc(config.headers[j])}">${j===row.length-1?(config.target?link(config.target,cell,'table-action'):button(cell,'demo','table-action')):j===0?`<strong>${esc(cell)}</strong>`:/Status|state|Severity|Priority|Result|Validation/.test(config.headers[j])?badge(cell):esc(cell)}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`;
}
function opsTable() {
  const config=tableConfigs[p.id] || tableConfigs.catalog;
  return `${heading(config.action?(config.actionTarget?link(config.actionTarget,config.action,'primary'):button(config.action,'demo','primary')):button('Export demo list'))}<div class="toolbar"><label class="search-inline">${icon('search')}<input id="table-search" type="search" placeholder="Filter these sample rows" aria-label="Filter table"></label>${badge('Synthetic data')}${button('Refresh preview')}</div>${dataTable(config)}<p id="table-count" class="fine-print" role="status">${config.rows.length} sample rows · No live database is connected.</p>`;
}
function dashboard() {
  const seo=p.id==='seo';
  return `${heading(link(seo?'seo-issues':'collector',seo?'Inspect issues':'Open review inbox','primary'))}<div class="dashboard-grid"><section class="focus-panel"><span class="edition">${seo?'Index quality':'Next in your queue'}</span><h2>${seo?'Keep private pages private.':'Good records start with a careful review.'}</h2><p>${seo?'Inspect publication state, canonical URLs and metadata before making pages indexable.':'Review provenance and rights before publishing. Collector candidates are never public by default.'}</p>${link(seo?'seo-issues':'candidate',seo?'Review indexability':'Review next candidate','primary')}<div class="queue-preview">${works.slice(0,3).map((w,i)=>`<div><span class="mini-art art-${w.color}"></span><span><strong>${w.title}</strong><small>${i===1?'Conflicting release dates':'Awaiting source review'}</small></span>${badge('Private')}</div>`).join('')}</div></section><aside class="panel attention-panel"><h2>Needs attention</h2>${[['rights','Rights & safety','3 open cases'],['conflicts','Metadata conflicts','2 unresolved'],['media','Asset evidence','4 awaiting review']].map(([id,title,count])=>`<a class="attention-row" href="${href(id)}"><span>${title}<strong>${count}</strong></span>${icon('arrow')}</a>`).join('')}<p class="fine-print">Illustrative counts, not live metrics.</p></aside></div><section>${sectionTitle('Recent activity', 'audit')}${dataTable(tableConfigs.audit)}</section>`;
}
function editorPage() {
  const performer=p.id==='new-performer', studio=p.id==='new-studio', ads=p.id==='ads';
  return `${heading(button('Preview changes','demo')+button('Request demo review','demo','primary'))}<div class="editor-layout"><form class="panel stack-form" data-form="demo"><h2>${ads?'Placement details':'Core information'}</h2>${input(performer?'Display name':studio?'Studio name':ads?'Placement name':'Title','title','text',p.id==='catalog-detail'?'After the rain':'',true)}<div class="field-grid">${input(performer||studio?'Public slug':'Catalogue number','code','text',p.id==='catalog-detail'?'SD-026':'',true)}${input('Source URL','source','url')}</div><label class="field">${ads?'Page placement':'Related studio'}<select><option>Select a ${ads?'placement':'studio'}</option>${(ads?['Homepage · labeled placement','Search · labeled placement']:studios).map(s=>`<option>${s}</option>`).join('')}</select></label><label class="field">Editorial notes<textarea rows="5" maxlength="2000" placeholder="Describe the change and its evidence"></textarea></label><button class="primary">Save demo draft</button><p class="form-status" role="status"></p></form><aside class="panel publication-panel"><h2>Publication checks</h2>${badge('Private draft')}<ul class="checklist"><li>Source provenance attached</li><li>Image rights reviewed</li><li>Age / consent obligations reviewed where applicable</li><li>Conflicts resolved</li><li>Authorized reviewer approval</li></ul><div class="notice">These checks are design requirements, not automated compliance decisions.</div>${button('Discard demo changes','delete')}</aside></div>`;
}
function reviewPage() {
  return `${heading(button('Hold candidate','demo')+button('Preview approved merge','demo','primary'))}<div class="notice">Private candidate · Comparing fictional metadata. A merge requires an explicit field decision.</div><div class="comparison-grid"><section class="panel"><h2>Collected candidate</h2><span class="source-label">Demo provider · received today</span><dl class="metadata"><div><dt>Title</dt><dd>After the rain</dd></div><div><dt>Release date</dt><dd>2026-09-25 ${badge('Conflict')}</dd></div><div><dt>Studio</dt><dd>Northlight Studio</dd></div></dl></section><section class="panel"><h2>Canonical record</h2><span class="source-label">SD-026 · existing catalogue</span><dl class="metadata"><div><dt>Title</dt><dd>After the rain</dd></div><div><dt>Release date</dt><dd>2026-09-24</dd></div><div><dt>Studio</dt><dd>Northlight</dd></div></dl></section></div><form data-form="demo" class="panel stack-form"><fieldset><legend>Resolve the release date</legend><label class="check"><input type="radio" name="decision" value="keep" required>Keep canonical value: 2026-09-24</label><label class="check"><input type="radio" name="decision" value="replace" required>Use candidate value: 2026-09-25</label><label class="check"><input type="radio" name="decision" value="hold" required>Hold for source verification</label></fieldset>${input('Reason for this decision','reason','text','',true)}<button class="primary">Preview resolution</button><p class="form-status" role="status"></p></form>`;
}
function mediaPage() {return `${heading(button('Preview upload safeguards','demo','primary'))}<div class="notice">All artwork below is an abstract CSS placeholder. No actual performer or licensed work image is represented.</div><div class="media-grid">${works.slice(0,6).map((w,i)=>`<article class="panel">${poster(w,i)}<h2>${w.code} / Abstract cover</h2><p>Source: original layout artwork<br>Usage: demo only</p><div class="actions">${link('media-usage','Inspect usage','secondary')}${button('Remove','delete')}</div></article>`).join('')}</div>`;}
function rightsPage() {return `${heading(button('Preview intake settings'))}<div class="notice urgent">Safety triage first. Suspected underage and non-consensual content concerns require a documented, staffed response process.</div><div class="dashboard-grid"><section>${dataTable({headers:['Case','Concern','Priority','State','Action'],rows:[['R-012','Non-consensual image concern','Urgent','Needs triage','Review case'],['R-011','Rights ownership claim','High','Evidence requested','Review case'],['R-010','Incorrect attribution','Normal','Resolved','Review case']]})}</section><aside class="panel"><h2>Case R-012</h2>${badge('Fictional case')}<ol class="steps"><li>Check immediate safety risk</li><li>Restrict access when appropriate</li><li>Record evidence securely</li><li>Respond and document outcome</li></ol><div class="actions">${button('Preview restriction','delete','primary')}${button('Assign reviewer')}</div><p class="fine-print">No real reports, evidence or statutory timers are loaded.</p></aside></div>`;}
function boardPage() {return `${heading(button('Create demo collection','demo','primary'))}<div class="board">${['Draft selections','Ready for review','Published selections'].map((label,i)=>`<section class="panel"><h2>${label}</h2>${works.slice(i*2,i*2+2).map((w,j)=>`<article class="board-card">${poster(w,j)}<h3>${w.title}</h3><p>Fictional editorial collection</p>${button('Review selection')}</article>`).join('')}</section>`).join('')}</div>`;}
function analyticsPage() {return `${heading(button('Export demo report'))}<div class="notice">Illustrative aggregate numbers only. No analytics service or cookies are connected.</div><section class="panel"><div class="section-heading"><h2>Catalogue visits</h2><span>Sample week · UTC</span></div><div class="bar-chart" role="img" aria-label="Synthetic daily visits: Monday 120, Tuesday 180, Wednesday 150, Thursday 240, Friday 210, Saturday 300, Sunday 270">${[120,180,150,240,210,300,270].map((n,i)=>`<div><span>${n}</span><i style="--bar:${n/3}%"></i><small>${['Mon','Tue','Wed','Thu','Fri','Sat','Sun'][i]}</small></div>`).join('')}</div></section><section class="panel"><h2>What this report excludes</h2><p>Individual viewing history, identity documents, raw IP addresses and sensitive personal data should not become casual dashboard metrics. Final analytics design requires a privacy review.</p></section>`;}
function statePage() {
  const content={empty:['Nothing here yet','Try a different filter or explore the catalogue. No records have been removed.','Explore catalogue'],loading:['Loading this view','This is a paused loading-state design, not a live request.','Show loaded preview'],error:['This view could not load','Your work has not been changed. Try again or return to the overview.','Retry preview'],denied:['Access is unavailable','This account or region is not eligible for this view. No restricted content is shown.','Return to overview']}[state];
  return `${heading()}<section class="state-panel panel"><span class="state-symbol" aria-hidden="true">${state==='error'?'!':state==='denied'?'—':state==='loading'?'…':'○'}</span><h2>${content[0]}</h2><p>${content[1]}</p>${state==='loading'?'<div class="skeleton" aria-hidden="true"></div><div class="skeleton short" aria-hidden="true"></div>':''}<a class="primary" href="${href(state==='loading'||state==='error'?p.id:'home')}">${content[2]}</a>${link('report','Report a concern','inline-link','public')}</section>`;
}
function renderPage() {
  if(state!=='ready') return statePage();
  const renderers={home,works:worksPage,people:peoplePage,studios:peoplePage,work:detailPage,person:detailPage,studio:detailPage,auth:authPage,account:accountPage,settings:settingsPage,feedback:feedbackPage,policy:policyPage,age:agePage,report:reportPage,cookies:cookiesPage,dashboard,table:opsTable,editor:editorPage,review:reviewPage,media:mediaPage,rights:rightsPage,board:boardPage,analytics:analyticsPage,logout:()=>`${heading()}<section class="panel narrow"><h2>Leave this demo session?</h2><p>This previews the sign-out step. No real session will change.</p><div class="actions">${link('login','Preview signed-out screen','primary')}${link('home','Keep browsing','secondary')}</div></section>`};
  return renderers[p.kind]();
}

document.querySelector('#app').innerHTML = `<div class="prototype-banner">Design preview · Fictional data · No real operations${p.proposed?' · Proposed new page':''}</div>${header()}<main id="main" tabindex="-1">${renderPage()}</main>${footer()}${surface==='ops'?'</div></div>':''}`;
const toast = message => {const el=document.querySelector('#toast');el.textContent=message;el.classList.add('visible');clearTimeout(toast.timer);toast.timer=setTimeout(()=>el.classList.remove('visible'),4500);};
let confirmTrigger;
document.addEventListener('click', event=>{
  const el=event.target.closest('[data-action]');if(!el)return;
  const action=el.dataset.action;
  if(action==='save') {const saved=el.getAttribute('aria-pressed')==='true';el.setAttribute('aria-pressed',String(!saved));toast(saved?'Removed from this demo selection.':'Saved in this preview only.');}
  else if(action==='delete') {confirmTrigger=el;document.querySelector('#confirm-dialog').showModal();}
  else if(action==='cancel'||action==='confirm') {document.querySelector('#confirm-dialog').close();confirmTrigger?.focus();if(action==='confirm')toast('Demo action confirmed. No business data was changed.');}
  else if(action==='filter') {el.parentElement.querySelectorAll('button').forEach(b=>b.setAttribute('aria-pressed',String(b===el)));const items=el.textContent==='New releases'?works.filter(w=>w.year==='2026'):el.textContent==='Recently indexed'?[...works].reverse():works;document.querySelector('.work-grid').outerHTML=workCards(6,items);toast(`${el.textContent}: sample selection updated.`);}
  else if(action==='alphabet') {el.parentElement.querySelectorAll('button').forEach(b=>b.setAttribute('aria-pressed',String(b===el)));const ranges={'A–F':['A','F'],'G–M':['G','M'],'N–S':['N','S'],'T–Z':['T','Z']};document.querySelectorAll('.person-card').forEach(card=>{const range=ranges[el.textContent];card.hidden=!!range&&(card.dataset.name[0]<range[0]||card.dataset.name[0]>range[1]);});}
  else if(action==='reset-filters') {document.querySelectorAll('[data-filter]').forEach(select=>select.selectedIndex=0);document.querySelector('#archive-search').value='';filterWorks();}
  else if(action==='reject-cookies'||action==='save-cookies') {if(action==='reject-cookies')document.querySelectorAll('.narrow input[type=checkbox]').forEach(box=>box.checked=false);document.querySelector('#privacy-status').textContent='Demo choices updated. No optional network requests or live consent records were created.';}
  else if(action==='age-demo') toast('Verification hand-off preview only. Restricted content remains unavailable.');
  else toast('Interaction preview only. No request was sent and no live data was changed.');
});
function filterWorks() {
  const term=document.querySelector('#archive-search')?.value.toLowerCase().trim()||'';
  const studio=document.querySelector('[data-filter=studio]')?.value||'';
  const year=document.querySelector('[data-filter=year]')?.value||'';
  let list=works.filter(w=>(!studio||w.studio===studio)&&(!year||w.year===year)&&`${w.title} ${w.code} ${w.studio}`.toLowerCase().includes(term));
  if(document.querySelector('[data-filter=sort]')?.value==='title') list.sort((a,b)=>a.title.localeCompare(b.title));
  document.querySelector('#results').innerHTML=list.length?workCards(8,list):'<div class="panel"><h2>No matching sample records</h2><p>Try another search or reset the filters.</p></div>';
  document.querySelector('#result-count').textContent=`${list.length} fictional records`;
}
document.addEventListener('submit',event=>{
  const form=event.target.closest('form[data-form]');if(!form)return;event.preventDefault();
  if(form.dataset.form==='search') {if(document.querySelector('#results')) filterWorks();else {const next=new URL(href('search','public'),location.href);next.searchParams.set('q',form.elements.query.value);location.href=next.href;}}
  else {form.querySelector('.form-status').textContent=form.dataset.form==='report'?'Demo receipt: R-DEMO. No report was sent. This is not an active reporting channel.':'Demo step complete. Nothing was transmitted or saved to the application.';}
});
document.addEventListener('change',event=>{
  if(event.target.matches('[data-filter]'))filterWorks();
  if(event.target.id==='age-state') {const variants={pending:['Awaiting verification','An approved provider must confirm eligibility before restricted content is available.'],unavailable:['Verification unavailable','Try again later or contact support. The restricted content stays locked; no fallback bypass is offered.'],restricted:['Not available in this region','This service cannot provide restricted content for the selected region. Reporting and privacy information remain available.']};const [title,body]=variants[event.target.value];document.querySelector('#age-result').innerHTML=`<strong>${title}</strong><p>${body}</p>`;}
});
document.querySelector('#table-search')?.addEventListener('input',event=>{let count=0;document.querySelectorAll('tbody tr').forEach(row=>{row.hidden=!row.textContent.toLowerCase().includes(event.target.value.toLowerCase());if(!row.hidden)count++;});document.querySelector('#table-count').textContent=`${count} matching sample rows`;});
if(query.get('q')&&document.querySelector('#results')){document.querySelector('#archive-search').value=query.get('q').slice(0,100);filterWorks();}
if(window.parent!==window)window.parent.postMessage({type:'deepsearch-preview',theme:t.id,surface,page:p.id,state},location.origin);
