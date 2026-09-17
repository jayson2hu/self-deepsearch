# OPT-00 基线与页面可靠性

实现提交：`ec0ac54`。前端 smoke 读取完整响应体，识别 Next.js/React 流式错误标记；新增 HTTP 200 错误正文负例。API origin/环境预检合同通过，测试栈公开首页和后台登录页均为 200 且错误标记缺失。OA-01 PASS；OA-02 因无真实浏览器为 PARTIAL。
