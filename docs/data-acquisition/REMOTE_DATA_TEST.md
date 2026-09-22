# 独立仓库与另一台电脑的数据测试

本次将采集设计、离线管线、网站任务配置、交互预览与测试抽取为独立私有仓库：[jayson2hu/self-deepsearch-collector](https://github.com/jayson2hu/self-deepsearch-collector)。已推送到 `main`，首次交付提交为 [`2fea405`](https://github.com/jayson2hu/self-deepsearch-collector/commit/2fea4051accf1f53d0b3c804a1ba6cfd9ddc7a62)。

完整的 Windows / Linux / macOS 安装、fixture、代理访问、少量真实页面取样与结果回传步骤在[远端电脑测试手册](https://github.com/jayson2hu/self-deepsearch-collector/blob/main/docs/REMOTE_DATA_TEST.md)，配套[测试结果模板](https://github.com/jayson2hu/self-deepsearch-collector/blob/main/docs/TEST_RESULT_TEMPLATE.md)。

当前可直接运行合成管线与浏览器演示；真实网站解析器尚未接入。另一台电脑可以按手册核验来源访问并采集少量允许使用的 HTML 样本，再据样本开发真实字段解析。网站登记和计划保存本身不会启用网络抓取。

独立仓库只包含采集相关代码，不包含正在修改的公开站、账号偏好与正式 API 代码；导出文件清单与来源基线记录在独立仓库的 `SOURCE_MANIFEST.json`。本仓库与独立仓库的后续改动不会自动双向同步。
