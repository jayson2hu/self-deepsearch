# RD v1 / DEV-05 开发与待验收记录

日期：2026-09-16（Asia/Shanghai）；执行人：Codex；状态：实现完成，状态/生产 HTTP/部署合同通过，真实浏览器视觉矩阵待验收。基线提交：`0af611a`。

## 实际交付

- `AdSlot` 从固定测试占位改为 `empty / ready / error / preview` 状态层；默认配置为 `empty`，empty/error 直接返回空，不输出 `.ad-slot`、广告文案或固定高度。
- 无素材时左右 rail 和页脚包装自动折叠，主内容恢复单列，不因保留调用点产生桌面空白；首页、列表、三类详情、账号/认证页共享同一规则。
- `ready` 只接受显式启用、唯一槽位、固定位置尺寸、RFC3339 有效期内素材、允许域名 HTTPS 目的地以及受限本地 `/ads/` 或允许域名图片；未知字段、重复槽位、脚本字段、路径穿越、外域、HTTP、凭据 URL、过期和错误尺寸全部 fail closed。
- ready 广告具有“广告”标识、广告主和可访问名称；外链固定 `noopener noreferrer sponsored`。组件不读取登录状态，因此访客与登录用户使用同一可用性规则。
- `preview` 必须由调用方显式请求，并同时满足非 production 与 `DISPLAY_AD_PREVIEW_FIXTURE=enabled`；生产 Compose 不注入该变量。预览使用本地合成矢量素材，无目的地，不代表真实投放。
- 日本 display-web Compose 和环境样例新增广告开关、JSON、素材/目的地允许域名；全部默认关闭。没有新增数据库、API、第三方 SDK、追踪、结算或广告管理后台。

## 配置合同

`DISPLAY_ADS_ENABLED=false` 是默认和统一回退。启用时 `DISPLAY_AD_SLOTS_JSON` 是最多 20 项的数组，每项只允许：`slot`、`placement`、`title`、`sponsor`、`image_url`、`image_alt`、`target_url`、`width`、`height`、`starts_at`、`ends_at`。槽位键为 `placement:页面传入标签`；尺寸固定为 banner/footer 960×90、rail 160×600、infeed 640×120。

远程图片主机必须列入 `DISPLAY_AD_ASSET_HOSTS`，目的地主机必须列入 `DISPLAY_AD_TARGET_HOSTS`，均只接受 HTTPS 精确 hostname。仓库内经审核素材只能位于 `/ads/`，不接受 `..`、查询、片段或反斜杠。配置改变需按部署流程重启/替换前端制品并重新执行本记录的两类 HTTP smoke。

## 验证结果

| 命令/环境 | 结果 | 范围 |
| --- | --- | --- |
| `node --test scripts/tests/redesign_ad_policy.test.mjs` | 5/5 通过 | 四态、默认关闭、时间/尺寸/URL/字段/路径负例、认证无分支和无 HTML/script 注入 |
| display-web typecheck / lint / production build | 通过 | 类型、静态规则及生产制品 |
| `npm run smoke:frontend:release-a` | 通过 | 默认 empty 的生产首页不含广告 DOM/旧占位，正文、安全头和代理保持可用 |
| `node scripts/redesign_ad_http_smoke.mjs` | 通过 | 临时注入一条本地合成 ready 素材；匿名与登录 Cookie 均输出同一 1 个 ready 槽位，无 preview/第三方脚本 |
| Python 3.12 产品/部署合同 | 51/51 通过 | Compose 环境白名单、默认关闭、部署探针与 DEV-02/03 当前首页/详情合同 |
| Playwright 用例发现 | 84 条成功 | 更新默认无广告 DOM、rail/footer 隐藏规格；因本机无 Chrome/Chromium 未执行浏览器 |
| Markdown 链接 / `git diff --check` | 通过 | 95 份 Markdown、669 个本地链接、文本差异无空白错误 |

HTTP smoke 只启动本机回环临时进程与内存合成 API；ready 夹具配置不写入环境文件，进程结束即消失。两份 SVG 均明确标为测试素材，不是商用创意或供应商确认。

## 未关闭门禁

- AC-07 的生产 DOM 默认空态、首页和共享页脚实现已通过 HTTP；仍缺 320/390/768/1440 浏览器截图与几何断言，无法在当前无浏览器环境证明全部页面没有视觉遗留空白。
- AC-08 的 empty/error/preview 负例和 ready 生产 HTML 已通过；尚未用真实浏览器加载图片、检查布局稳定性、键盘访问、外链行为及有效期切换，也没有真实广告素材/供应方授权，因此真实广告继续关闭。
- Q-05 仍未确认。不得把合成 ready smoke、预览 SVG 或环境接口存在解释为批准广告上线。

因此 DEV-05 标记“待验收”，不计入已验收完成数。

## 回退

设置 `DISPLAY_ADS_ENABLED=false` 并替换/重启 display-web，即统一回到 empty；正文不依赖广告配置或素材成功。不能通过公开查询参数开启 preview，也不能为恢复旧外观重新输出无素材占位。
