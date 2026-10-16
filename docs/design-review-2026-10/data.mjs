// Shared, dependency-free design inventory. All records and numbers are synthetic.
const theme = (id, name, english, family, layout, ops, colors, display, body, radius, columns, description) => ({
  id, name, english, family, layout, ops, colors, display, body, radius, columns, description,
});
export const themes = [
  theme('01', '午夜首映', 'Midnight Premiere', '深色', 'premiere', 'sidebar', ['#111820','#1b2632','#f4f3ef','#b6c4cf','#d4ab71','#171a1f'], 'Georgia', 'Arial', 4, 4, '左右分屏首映封面，横向宽幅作品，深色运营工作台。'),
  theme('02', '编辑手记', 'Folio', '编辑', 'folio', 'topbar', ['#f8f6f0','#ffffff','#252922','#5f6559','#315345','#ffffff'], 'Palatino Linotype', 'Arial', 0, 3, '刊物式大标题与非对称精选，运营端采用双栏编审。'),
  theme('03', '瑞士索引', 'Swiss Index', '高效', 'swiss', 'topbar', ['#f4f5f6','#ffffff','#182026','#59616a','#ae2d20','#ffffff'], 'Arial', 'Arial', 0, 3, '严格三分网格、大字检索和红色索引，紧凑横向导航。'),
  theme('04', '藏品档案', 'Saffron Archive', '复古', 'archive', 'sidebar', ['#f5e8c9','#fff7e3','#302919','#65563b','#765018','#ffffff'], 'Georgia', 'Georgia', 2, 3, '档案目录侧栏、纸质条目和藏品编号，阅读优先。'),
  theme('05', '私藏书架', 'Evergreen', '柔和', 'shelf', 'split', ['#eaf0eb','#f8fbf8','#173d32','#4e675c','#235943','#ffffff'], 'Palatino Linotype', 'Verdana', 10, 4, '书架式方形卡片与窄导航，收藏和审阅详情并重。'),
  theme('06', '装饰剧院', 'Grand Marquee', '深色', 'marquee', 'topbar', ['#191b25','#252634','#f4dfac','#c0b99f','#e4c06b','#252117'], 'Georgia', 'Arial', 0, 3, '对称剧院封面和几何细线，居中运营任务面板。'),
  theme('07', '白纸留白', 'Shiro', '极简', 'shiro', 'rail', ['#ffffff','#f5f5f2','#262b29','#5e665f','#354e49','#ffffff'], 'Georgia', 'Arial', 0, 2, '双列作品与大量留白，表单和阅读无干扰。'),
  theme('08', '信号实验', 'Signal', '实验', 'signal', 'topbar', ['#eef248','#ffffff','#20251a','#515733','#242b20','#ffffff'], 'Arial Black', 'Arial', 0, 3, '硬边拼贴、巨幅字体和醒目信号色，模块任务板。'),
  theme('09', '夜间频道', 'Ultraviolet', '深色', 'channel', 'sidebar', ['#17132e','#241e42','#f7f1ff','#c3b7da','#c7a8ff','#26163d'], 'Trebuchet MS', 'Arial', 18, 4, '双封面频道流，紫色深夜界面和分组事件流。'),
  theme('10', '石板控制台', 'Slate Console', '高效', 'console', 'sidebar', ['#eaf0f5','#ffffff','#203447','#516579','#28567c','#ffffff'], 'Verdana', 'Arial', 6, 4, '以检索和过滤为首页，资料密度高，后台标准控制台。'),
  theme('11', '柔光画廊', 'Soft Focus', '柔和', 'soft', 'topbar', ['#f8edf1','#fffafd','#4d293c','#765467','#8a315c','#ffffff'], 'Georgia', 'Trebuchet MS', 24, 3, '柔和双栏画廊、宽松间距，内容与操作都容易扫读。'),
  theme('12', '每日纪事', 'Daily Record', '编辑', 'news', 'topbar', ['#f3f0e8','#fcfaf4','#302c25','#666055','#6e3c28','#ffffff'], 'Times New Roman', 'Arial', 0, 4, '报刊多栏、更新日期与信息摘要，运营采用时间线。'),
  theme('13', '参考图书馆', 'Reference Library', '复古', 'library', 'split', ['#f0eae0','#fffdf8','#31362c','#666b5b','#405a31','#ffffff'], 'Palatino Linotype', 'Verdana', 3, 4, '分类抽屉和目录卡，元数据可信度与检索优先。'),
  theme('14', '钴蓝工作室', 'Cobalt Studio', '实验', 'cobalt', 'topbar', ['#eef2ff','#ffffff','#182e67','#53658f','#214db7','#ffffff'], 'Arial', 'Arial', 0, 3, '巨字与窄封面比例，钴蓝横栏和可分组工作面板。'),
  theme('15', '砂岩目录', 'Sandstone', '柔和', 'sandstone', 'sidebar', ['#f1e6d7','#fff8ef','#43382e','#716152','#795039','#ffffff'], 'Georgia', 'Verdana', 14, 3, '横幅封面、纵向阅读与宽导航，编辑表单双栏。'),
  theme('16', '幕外画册', 'Offscreen', '编辑', 'offscreen', 'rail', ['#e9eef1','#ffffff','#203344','#596c7b','#375267','#ffffff'], 'Georgia', 'Arial', 0, 2, '非对称大封面与边缘索引，精选画册感。'),
  theme('17', '数据图鉴', 'Data Atlas', '高效', 'atlas', 'split', ['#e7f1f3','#f8fdfe','#123d49','#4d6972','#136276','#ffffff'], 'Courier New', 'Arial', 0, 4, '结构化字段与可筛选条目，双导航审阅队列。'),
  theme('18', '无障碍极简', 'Essential', '极简', 'essential', 'rail', ['#ffffff','#f1f1f1','#000000','#515151','#000000','#ffffff'], 'Verdana', 'Verdana', 0, 2, '单轴内容、清晰边界和强对比，减少视觉负担。'),
  theme('19', '胶片放映室', 'Analog Room', '复古', 'analog', 'sidebar', ['#28231f','#373029','#f9e8ce','#c9b79b','#e0ae70','#2e2419'], 'Georgia', 'Arial', 2, 4, '暖色胶片横列与目录，审阅队列延续胶片索引。'),
  theme('20', '开放书架', 'Open Shelf', '高效', 'open', 'split', ['#f0f5fb','#ffffff','#22364d','#536a83','#2b5f9c','#ffffff'], 'Trebuchet MS', 'Arial', 12, 4, '左侧搜索与右侧书架组合，运营采用易扫读任务板。'),
];

const page = (id, route, title, kind, description, group = 'Discover', proposed = false) => ({id, route, title, kind, description, group, proposed});
export const pages = {
  public: [
    page('home','/','Discover the archive','home','Find works, performers and studios. An independent metadata index for adults.'),
    page('search','/search','Search the archive','works','Search by title, catalogue number, performer or studio.'),
    page('latest','/latest','New releases','works','Works ordered by release date. Release dates and catalogue additions are kept separate.'),
    page('recent','/recent','Recently indexed','works','New additions to the catalogue, regardless of release date.'),
    page('popular','/popular','Popular in the archive','works','An illustrative popularity view. No real usage data is shown.'),
    page('work','/works/[slug]','After the rain','work','A synthetic record used to review the work detail layout.'),
    page('performers','/performers','Performer index','people','Browse names and credited works. All names here are fictional.'),
    page('performer','/performers/[slug]','Alex Morgan (fictional)','person','Fictional profile for layout review. No portrait or identity claim.'),
    page('studios','/studios','Studio directory','studios','Explore fictional studios and their catalogue records.'),
    page('studio','/studios/[slug]','Northlight Studio (fictional)','studio','A fictional studio page for reviewing information hierarchy.'),
    page('login','/login','Welcome back','auth','Sign in to manage your own library. Do not enter real credentials.', 'Account'),
    page('register','/register','Create your library','auth','Prototype only. Registration and age eligibility are separate checks.', 'Account'),
    page('forgot','/forgot-password','Reset your password','auth','Use a fictional email address to review this flow.', 'Account'),
    page('invite','/invite','Your invitation','auth','Check an invitation before joining. Demo tokens only.', 'Account'),
    page('logout','/logout','Sign out','logout','End this demo session on this device.', 'Account'),
    page('account','/account','Your library','account','A private place for the works and names you want to revisit.', 'Account'),
    page('settings','/account/settings','Account & privacy','settings','Control your profile, sessions and data preferences.', 'Account'),
    page('feedback','/account/feedback','Send feedback','feedback','Tell us about incorrect metadata or an accessibility issue.', 'Account'),
    page('follows','/account/follows','Following','people','Fictional performers and studios you follow.', 'Account'),
    page('follow-works','/account/follows/works','From your follows','works','New catalogue records from names you follow.', 'Account'),
    page('history','/account/history','Browsing history','works','Private history preview. Retention policy is not yet approved.', 'Account'),
    page('favorites','/account/favorites','Saved works','works','Your private list. Saved items are never shown publicly.', 'Account'),
    page('hidden','/account/hidden','Hidden works','works','Manage records you have chosen to hide.', 'Account'),
    page('about','/about','About this archive','policy','An adult metadata index, not a streaming or download service.', 'Trust'),
    page('privacy','/privacy','Privacy notice','policy','Draft layout only. Actual controller, vendors and retention need approval.', 'Trust'),
    page('age','/age-assurance','Age & access','age','Proposed flow: private proof of eligibility before restricted content.', 'Trust', true),
    page('report','/report','Report a concern','report','Proposed public reporting route. No account should be required.', 'Trust', true),
    page('cookies','/cookie-preferences','Privacy choices','cookies','Proposed preferences. Optional tracking starts off.', 'Trust', true),
  ],
  ops: [
    page('home','/','Workspace overview','dashboard','Review what needs attention before anything becomes public.', 'Workspace'),
    page('login','/login','Operator sign in','auth','Restricted workspace. Use only fictional credentials in this prototype.', 'Workspace'),
    page('logout','/logout','Sign out of workspace','logout','End this illustrative operator session.', 'Workspace'),
    page('catalog','/catalog','Catalogue','table','Review publication status, provenance and rights for every record.', 'Content'),
    page('catalog-detail','/catalog/[entityType]/[entityID]','Edit catalogue record','editor','After the rain · SD-026 · synthetic record.', 'Content'),
    page('new-work','/works/new','Create work','editor','Add metadata and evidence before requesting publication.', 'Content'),
    page('new-performer','/performers/new','Create performer','editor','Names and credits only; no fabricated identity evidence.', 'Content'),
    page('new-studio','/studios/new','Create studio','editor','Add a studio profile with documented source information.', 'Content'),
    page('imports','/imports','Import jobs','table','Validate a batch and preview changes before importing.', 'Collection'),
    page('collector','/collector','Collection inbox','table','Candidates remain private until reviewed and published.', 'Collection'),
    page('candidate','/collector/[candidateID]','Review candidate','review','Compare collected metadata with the canonical catalogue record.', 'Collection'),
    page('conflicts','/conflicts','Resolve conflicts','review','Choose field values explicitly; preserve a clear audit trail.', 'Collection'),
    page('editorial','/editorial','Editorial selections','board','Choose published works for public collections.', 'Content'),
    page('ads','/ads','Advertising placements','editor','Preview placement, labels and consent dependencies. No real ads.', 'Content'),
    page('media','/media','Media library','media','Review licensed assets and identify missing evidence.', 'Media'),
    page('media-usage','/media/usage','Asset usage','table','See where assets are used before replacing or removing them.', 'Media'),
    page('upload-control','/media/upload-control','Upload controls','settings','Restrict file types and quarantine unreviewed uploads.', 'Media'),
    page('users','/users','User management','table','Access is role-based. Only synthetic users appear here.', 'Trust'),
    page('feedback','/feedback','Feedback inbox','table','Triage metadata and accessibility feedback.', 'Trust'),
    page('rights','/rights','Rights & safety desk','rights','Prioritise consent, suspected underage content and ownership concerns.', 'Trust'),
    page('audit','/audit','Audit trail','table','Accountable changes, with sensitive values redacted.', 'Trust'),
    page('settings','/settings','Workspace settings','settings','Environment, access and publication safeguards.', 'Workspace'),
    page('seo','/seo','Search visibility','dashboard','Published, eligible content only. Private routes remain excluded.', 'Insights'),
    page('seo-issues','/seo/issues','Indexing issues','table','Inspect canonical URLs, metadata and indexability.', 'Insights'),
    page('seo-reports','/seo/reports','SEO reports','table','Track completed inspections, not guarantees of search ranking.', 'Insights'),
    page('traffic','/traffic','Traffic overview','analytics','Synthetic aggregate activity, with no real visitor tracking.', 'Insights'),
  ],
};
export const states = {ready:'正常', empty:'空数据', loading:'加载中', error:'服务异常', denied:'无权访问'};
export const works = [
  {title:'After the rain', code:'SD-026', studio:'Northlight', year:'2026', color:'clay', shape:'arch'},
  {title:'Quiet geometry', code:'SD-025', studio:'Form & Field', year:'2026', color:'olive', shape:'circle'},
  {title:'Blue hour', code:'SD-024', studio:'Daybreak', year:'2025', color:'blue', shape:'steps'},
  {title:'A room in amber', code:'SD-023', studio:'Northlight', year:'2025', color:'amber', shape:'window'},
  {title:'The other shore', code:'SD-022', studio:'Still House', year:'2025', color:'rose', shape:'circle'},
  {title:'Soft edges', code:'SD-021', studio:'Form & Field', year:'2024', color:'slate', shape:'arch'},
  {title:'Parallel lines', code:'SD-020', studio:'Daybreak', year:'2024', color:'olive', shape:'steps'},
  {title:'In plain sight', code:'SD-019', studio:'Still House', year:'2024', color:'clay', shape:'window'},
];
export const people = ['Alex Morgan','Sam Rivers','Taylor Quinn','Jordan Lane','Robin Ellis','Casey Reed'];
export const studios = ['Northlight','Form & Field','Daybreak','Still House'];
export function escapeHTML(value) {return String(value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));}
export function getTheme(id) {return themes.find(t => t.id === id) || themes[0];}
export function getPage(surface, id) {return (pages[surface] || pages.public).find(p => p.id === id) || pages[surface]?.[0] || pages.public[0];}
export function previewURL(themeID, surface, pageID, state = 'ready') {
  return `preview.html?${new URLSearchParams({theme:themeID, surface, page:pageID, state})}`;
}
