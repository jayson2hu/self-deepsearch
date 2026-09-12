# Release A：当前部署配置静态复核

日期：2026-09-11（Asia/Shanghai）  
结论：当前工作区的根级 Compose、日本 edge+monitoring 完整 profile、北京 Compose 与 Release A 五镜像 Bake 图均可由 Docker CLI 成功解析。全程没有启动容器、构建镜像层、连接数据库或访问外部服务，因此这是静态可部署性证据，不是运行态验收。

## 执行命令

```text
docker compose config --quiet
docker compose --env-file infra/compose/japan/.env.example -f infra/compose/japan/compose.yaml --profile edge --profile monitoring config --quiet
docker compose --env-file infra/compose/beijing/.env.example -f infra/compose/beijing/compose.yaml config --quiet
docker buildx bake release-a --print
```

结果：四个命令退出码均为 0。Bake 的 `release-a` 组精确包含：

- `platform-api`；
- `platform-worker`；
- `display-web`；
- `ops-web`；
- `media-python`。

五个目标都使用仓库根目录作为构建上下文、各自 Dockerfile、`BUILD_VERSION`/`BUILD_REVISION` 参数和独立镜像标签。没有执行 `--load`、`--push` 或普通 `docker compose up`。

## 尚未证明

- Dockerfile 镜像层可以在目标 Linux 主机完整构建；
- 容器健康检查、非 root 权限、卷所有权和资源限制在日本 2C4G/北京 2C8G 正常；
- 私有 env、Origin Certificate、PostgreSQL、SMTP、S3/R2、Cloudflare 和跨区网络配置正确；
- 备份、回滚、告警和最终 HTTPS 入口已经运行态通过。

因此当前部署包仍是 conditional-go，公网继续 no-go。真实环境按 [`RELEASE_A_RUNBOOK.md`](../RELEASE_A_RUNBOOK.md) 和 [`RELEASE_A_GO_LIVE_RECORD.md`](../RELEASE_A_GO_LIVE_RECORD.md) 执行，不得用静态解析结果替代目标环境探针。
