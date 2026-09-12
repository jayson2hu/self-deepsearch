# Release A：运营后台与公开账号页面请求级 CSP nonce

日期：2026-09-11（Asia/Shanghai）  
结论：运营后台及公开站登录、注册、重置、邀请、退出和账号中心 production HTML 已从静态 `script-src 'unsafe-inline'` 改为每请求 128 位随机 nonce + `strict-dynamic`，并保持 `private, no-store`。公开首页仍是 5 分钟 ISR，不因账号加固退化为全站动态渲染。

后续说明：本证据保留当时的构建结果。之后为避免生产镜像烘入构建期回环配置和合成目录，公开首页已改为请求时渲染；账号页 nonce 边界未改变，目录 API 数据缓存和公共入口 5 分钟缓存仍保留。当前结果见[运行时目录证据](./release-a-runtime-catalog-2026-09-11.md)。

## 实现边界

- `ops-web` Proxy 为每个 HTML 请求生成独立 32 位十六进制 nonce，同时写入请求 CSP、`x-nonce` 和响应 CSP；Next.js 将同一 nonce 附加到 bootstrap/RSC 可执行脚本。
- `display-web` 只对 `/login`、`/register`、`/forgot-password`、`/invite`、`/logout` 和 `/account/**` 使用同样的请求级 nonce；路径分类精确拒绝 `/login-help`、`/accounting` 等近似路径。原本静态的登录和邀请页改为动态渲染，其他账号页维持原动态边界。
- API、admin proxy、静态资源和 logout action 不经过 nonce Proxy；它们不返回可执行 HTML。其他浏览器安全头仍由 Next/Nginx 保留。
- 两站 `style-src 'unsafe-inline'` 暂时保留；本轮只移除账号与后台可执行脚本的 `unsafe-inline`。公开目录仍使用静态脚本策略，但 Next 与 Nginx 均显式增加 `script-src-attr 'none'`，不允许内联事件处理器。
- 公开首页为保持 ISR、SEO 和缓存性能继续使用静态 CSP；不能把账号页面加固写成全站风险关闭。
- 未启动 Docker、未连接 PostgreSQL/SMTP/R2/Cloudflare，也未验证最终 Nginx/Cloudflare 实际响应头合并。

## 验证

```text
npm run test:frontend:smoke
npm run typecheck --workspace @self-deepsearch/display-web
npm run lint --workspace @self-deepsearch/display-web
npm run build --workspace @self-deepsearch/display-web
npm run typecheck --workspace @self-deepsearch/ops-web
npm run lint --workspace @self-deepsearch/ops-web
npm run build --workspace @self-deepsearch/ops-web
npm run smoke:frontend:release-a
node scripts/run_python.mjs -m unittest infra.reverse-proxy.tests.test_nginx_contract
npm run test:e2e:release-a
```

结果：

- frontend smoke unit tests：38/38；其中两站 nonce 生成、公开账号路径分类、策略校验、畸形 nonce、响应轮换和目录静态边界均有专项覆盖；
- Nginx 合同：14/14；
- 两站 typecheck、ESLint 和 production build 通过；公开构建确认首页仍为静态 5 分钟 ISR，登录与邀请为动态页面；
- standalone HTTP 冒烟验证公开登录/注册和后台登录两次响应 nonce 不复用、策略无脚本 `unsafe-inline`、所有可执行脚本与响应 nonce 一致；
- Playwright desktop/mobile：76/76，通过账号、后台登录、审核发布、邀请权限、反馈下架、用量与上传控制等既有流程；新增用例确认账号页无 CSP 拒绝控制台错误，返回首页后仍是无 nonce 的静态目录策略。
- 最终再加严 `script-src-attr 'none'` 和 nonce HTML `no-store` 断言后，两站 CSP 用例按 desktop/mobile 定向复跑 4/4；随后当前工作区统一预检（使用已成功重建的两站 production 产物并显式 `--skip-build`）再次退出 0。

公网仍为 no-go：最终 HTTPS/Nginx/Cloudflare、固定 IP/VPN/Access、真实数据库和邮件环境均未验收。
