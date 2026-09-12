# Release A 本地视觉证据

生成日期：2026-09-10（Asia/Shanghai）  
证据范围：当前 Next.js production build + `release_a_visual_capture.mjs` 有状态合成 API + 本机 Chrome。  
非证据范围：真实 PostgreSQL、SMTP、S3/R2、Cloudflare、正式域名和真实资料。

## 重建方式

在仓库根目录执行：

```powershell
npm run capture:visual:release-a:build
```

已有当前 production build 时可使用 `npm run capture:visual:release-a`。命令只使用随机回环端口和内存合成状态，全部截图生成后自动关闭浏览器、mock API 与两个前端服务。[manifest.json](./manifest.json) 记录生成时间、合成边界、每个视口、文件字节数和 SHA-256；输出目录被限制在 `docs/evidence` 的独立子目录。

## 页面截图

- [公开首页桌面端](./display-home-desktop.png)：1440×900 视口，全页；
- [公开首页移动端](./display-home-mobile.png)：390×844 视口，全页；
- [作品详情桌面端](./work-detail-desktop.png)：1440×1000 视口，全页；
- [作品详情移动端](./work-detail-mobile.png)：390×844 视口，全页；
- [运营后台登录桌面端](./ops-login-desktop.png)：1440×900 视口，全页；
- [运营后台登录移动端](./ops-login-mobile.png)：390×844 视口，全页；
- [owner 运营概览桌面端](./ops-dashboard-desktop.png)：1440×900 视口，全页；
- [owner 运营概览移动端](./ops-dashboard-mobile.png)：390×844 视口，全页；
- [owner 用户权限桌面端](./ops-users-desktop.png)：1440×900 视口，全页；
- [owner 用户权限移动端](./ops-users-mobile.png)：390×844 视口，全页；
- [owner 审计查询桌面端](./ops-audit-desktop.png)：1440×900 视口，全页；
- [owner 审计查询移动端](./ops-audit-mobile.png)：390×844 视口，全页。

## 复核结论

- 桌面首页和详情页左右广告 rail 可见；移动端 rail 隐藏，顶部、信息流、详情和页脚占位不遮挡正文；
- 桌面与移动页面没有观察到横向溢出、坏图、正文被广告覆盖或页脚缺失；
- 作品无图片时使用本地默认作品图，详情页只展示一张主图，不制造大量截图负担；
- 页头保留游客登录/注册入口，页脚显示 18+、资料来源边界、资源边界、权利联系和广告说明；
- 游客关注区显示登录引导，不显示网络错误；首次截图由此发现 stateful mock 漏实现 `/api/v1/me/followed-works`，修复后已重新生成首页截图并增加桌面/移动浏览器回归；
- 运营后台登录表单在两种视口中完整显示，字段标签和提交按钮未被裁切；
- owner 登录后的运营概览、Release A 范围提示、用户权限操作和按 Request ID 查询审计时间线均已进入标准截图；审计页只展示操作者、动作、对象、理由与时间，不展示 revision 快照或原始 metadata；
- 审计查询在桌面与移动视口都保留完整标签、输入框、查询结果与权限边界说明，没有因长邮箱产生横向溢出；
- 捕获器会拒绝页面级横向溢出、可见坏图和浏览器脚本异常，只有 12 张截图全部完成后才更新证据与 manifest。

截图使用可正常搜索和浏览的 `TEST-001` 合成资料，后台登录态使用内存中的 `owner@example.test` 合成账号；单独的 `TEST-JSONLD` 安全夹具仍包含 `</script><script>alert(1)</script>`，只在自动回归中验证 JSON-LD 与可见文本转义，不再作为用户预览内容。截图只用于当前代码布局复核；真实资料、真实图片和目标 PostgreSQL 中的登录后运营数据仍需在目标环境重新验收。
