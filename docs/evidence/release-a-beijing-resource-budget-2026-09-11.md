# Release A：北京 2C8G 资源预算加固

日期：2026-09-11（Asia/Shanghai）  
结论：北京生产 Compose 原先没有 CPU、内存、PID 和日志轮转上限，图片解码或异常并发可能吃满整台 2C8G 主机。现已为两个常驻服务增加显式预算和自动合同；这只限制故障半径，不代表目标 Linux 已完成容量验收。

## 当前预算

| 服务 | CPU 上限 | 内存上限 | PID 上限 |
| --- | ---: | ---: | ---: |
| `platform-worker-beijing` | 0.20 | 192 MiB | 128 |
| `media-python` | 1.25 | 2048 MiB | 256 |
| 合计 | 1.45 | 2240 MiB | — |

宿主至少保留约 0.55 CPU 和 5 GiB 以上标称内存给 Linux、Docker、网络/SFTP、备份及人工工具。两个服务均使用 `json-file`，单文件 10 MiB、保留 3 份。

图片处理允许最大 10 MiB 输入和 4000 万像素；2 GiB 是保守起点，不是已测峰值。上线前必须用最大允许 JPEG/WebP、损坏图、并发删除和全量对账观察 RSS、CPU、OOM 与恢复，必要时降低图片像素/并发或调整预算，不能直接去掉上限。

## 合同

新增 `infra/security/tests/test_compose_resource_budget_contract.py`，持续检查：

- 日本完整 runtime 不超过 2 CPU/3 GiB；
- 北京两个服务均有 CPU/内存/PID 和日志限制；
- 北京常驻总额不超过 1.5 CPU/2304 MiB；
- 图片服务获得较高但有界的处理预算，Go Worker 保持小规格。

## 验证边界

```text
python -m pytest -q infra/security/tests/test_compose_resource_budget_contract.py
ruff check infra/security/tests/test_compose_resource_budget_contract.py
docker compose --env-file infra/compose/beijing/.env.example -f infra/compose/beijing/compose.yaml config --quiet
```

本轮没有启动容器、构建镜像、读取真实图片、连接 S3/R2 或执行跨区传输。运行态资源验收仍是公网 no-go 门槛。

定向合同 3/3、Ruff、北京 Compose 静态解析通过；随后重新执行默认统一预检，Python 全量 295 passed、288 subtests passed、1 skipped，两站 production build、standalone HTTP 与 Go→Next 缓存合同均通过。
