# Release A：Turnstile 站点与操作绑定

日期：2026-09-11（Asia/Shanghai）  
范围：公开站注册验证码、密码重置验证码、Go API 的 Cloudflare Turnstile Siteverify 调用和生产配置门禁。没有连接真实 Cloudflare、最终域名、SMTP 或 PostgreSQL。

## 风险与结论

旧实现只检查 Siteverify 返回的 `success=true`。这不能证明 token 来自本项目的最终站点，也不能区分“注册验证码”和“密码重置验证码”两个页面操作；外部响应也缺少正文大小、JSON 类型和重定向边界。

当前代码已把验证收紧为完整绑定：

- 注册组件固定签发 `signup_code`，密码重置组件固定签发 `password_code`；
- Go 路由把对应 action 传入验证器，验证器只接受这两个固定值，并与 Siteverify 响应中的 `action` 精确比较；
- 新增私密 API 配置 `TURNSTILE_EXPECTED_HOSTNAME`，生产启动只接受纯 DNS hostname；部署检查还要求它与 `SITE_URL` 的 hostname 一致；
- Siteverify 只使用固定官方 HTTPS endpoint，5 秒超时且禁止自动跟随重定向；只接受 HTTP 200 和 `application/json`；
- token 去除首尾空白后限制为 1～2048 字节，响应正文限制为 16 KiB；只有可解析 IP 才作为 `remoteip` 发送；
- 响应必须同时满足 `success=true`、hostname 大小写不敏感精确匹配、action 精确匹配。Cloudflare error codes、token 和 secret 均不进入业务日志。

开发绕过仍只接受本地固定 token `test-pass`，且同样要求路由使用已知 action。真实生产环境继续禁止 `TURNSTILE_BYPASS=true`。

## 本地验证

在工作区缓存下执行 Go 定向与全包测试、vet，通过以下场景：

- 正常请求的 secret、response、有效 remote IP，以及返回 hostname/action 绑定；
- 无效 remote IP 不发送；hostname 错误、action 错误和 `success=false` 拒绝；
- 空 token、超过 2 KiB token、未知 action 在联网前拒绝；
- 非 200、非 JSON、缺少 Content-Type、畸形 JSON、超过 16 KiB 响应拒绝；
- 302 不自动跟随，重定向目标未收到请求；
- 注册与密码重置路由分别传入不同固定 action；
- 生产配置拒绝缺失、URL、端口或非法标签形式的 expected hostname。

同时通过：

- display-web TypeScript typecheck 与 ESLint；
- frontend smoke 38/38；
- display-web 与 ops-web production build、standalone HTTP，以及真实 Go→Next 缓存合同；
- 环境检查与产品安全合同定向回归 63 passed、89 subtests passed。

## 未覆盖与上线条件

本轮没有向 Cloudflare 发起真实请求，因此只证明本地实现和配置合同。最终开放前仍需在 Cloudflare 中把真实 site key 限制到最终公开域名，服务器私有环境设置相同的 `TURNSTILE_EXPECTED_HOSTNAME`，并在最终 HTTPS 入口分别完成一次注册和密码重置验证；错误域名、错误 action、过期 token 和重复 token 都应被拒绝。该真实验收完成前，公网结论保持 no-go。
