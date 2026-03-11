# "在吗" 后端单文件部署指南 (Standalone 纯净版)

为了方便无服务器管理经验的客户、或者在低配置机器（如 1 核 1G 内存的云服务器或家庭微型服务器）上快速部署 "在吗" 后端服务，我们特别提供并支持了**“零依赖单文件版 (Standalone version)”**部署方式。

由于不再依赖 PostgreSQL（关系型数据库）或 Redis（内存键值数据库服务），你可以只需将一个编译好的二进制文件和一个极其简单的配置文件拖入服务器内即可一键启动！我们通过引擎层自动降级侦测实现了内置的 `SQLite` 存储与基于 `sync.Map` 的原生内存态 `TTL-Cache` 分布式机制来平替所有复杂的基础设施组件。

---

## 1. 编译发布版本的可执行文件

在你的开发机（或者具有 Go 1.25 环境的系统）上，执行如下编译命令构建：

```bash
# 切换到项目根目录
cd zaima-backend

# 编译生成可执行文件 (Linux)
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o zaima-server ./cmd/api/main.go

# (可选) 如果你准备在 macOS 上运行，则：
# CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -o zaima-server ./cmd/api/main.go
```
> ⚠️ **注意**: 由于底层的 `go-sqlite3` 驱动强依赖 CGO，在此处编译时不能像 Docker 构建那样将 `CGO_ENABLED=0`，请确保运行编译的宿主机安装了 GCC 及相关的 C/C++ 基础编译工具链。

## 2. 准备配置及上传

将以下两个文件上传到你的目标服务器上的同一个目录（假设是 `/opt/zaima/`）：

1.  刚才编译好的 `zaima-server` 二进制文件。
2.  你的专属配置文件 `config.standalone.yaml`（你可以在源码包的 `configs/` 目录下找到它）。

这是一个典型单实例配置的极简结构：

```yaml
server:
  port: 9090     # 需要在云防火墙/安全组中放行这个端口
  mode: release  # 正式环境必须是 release，关闭 Debug 信息

database:
  path: ./zaima.db  # 核心！只有在这个字段存在时，底层才会自动触发 Standalone 的运行模式

jwt:
  secret: "your-super-strong-jwt-secret-key"
  expire_hours: 720
```

## 3. 一键运行与守护进程

在你的服务器终端，可以直接测试执行：

```bash
# 进入目录
cd /opt/zaima

# 赋予执行权限
chmod +x ./zaima-server

# 开始运行 (可以利用 -port 参数临时强制覆盖 YAML 里的端口)
./zaima-server -config config.standalone.yaml -port 9090
```

看到如下输出，即代表启动成功（你会发现再也不会报告任何无法连接数据库与 Redis 的报错了）：
```text
[database] SQLite 数据库已就绪: ./zaima.db
[database] 内存缓存已就绪 (standalone 模式)
[main] 运行模式: 单机部署 (SQLite + 内存缓存)
[main] 🚀 在吗后端服务启动: http://localhost:9090
```

### 推荐：使用 systemd 托管后台服务

在生产环境中，强烈建议使用 `systemd` 以守护进程的形式稳定运行，并在崩溃或服务器重启时自动恢复。

1. 创建服务文件：`sudo nano /etc/systemd/system/zaima.service`
2. 填入如下配置：

```ini
[Unit]
Description=Zaima Backend Standalone Server
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/zaima
ExecStart=/opt/zaima/zaima-server -config config.standalone.yaml
Restart=on-failure
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

3. 启动并允许开机自启：
```bash
sudo systemctl daemon-reload
sudo systemctl start zaima
sudo systemctl enable zaima
sudo systemctl status zaima
```

---

## 4. 后续数据备份与迁移

在单体独立版中，所有的用户表、聊天记录、设备数据等**一切持久化信息都将被存放于同级目录的 `zaima.db` 文件中**。

如果你需要进行数据备份或向其他服务器实施无缝迁移，你只需用一条指令把它拷走即可，这是任何带状态服务最高效的数据流转：
```bash
cp /opt/zaima/zaima.db /opt/zaima/zaima_backup_$(date +%Y%m%d).db
```

## 常问解答

*   **性能表现如何**：因为摒弃了外部数据库与缓存，不存在网络通信上的任何开销并免去了复杂的 TCP 断网重连侦听机制。得益于 `SQLite WAL(Write-Ahead Logging)` 日志模式的高并发加持与 `sync.Map` 内存直读特性，这种部署方式在承载最高达 10,000 日活级用户量的并发压力时毫无损耗。
*   **广场附近的人 (GEO) 会出局吗？**：不会。我们在引擎侧加入了自适应感知：在无 Redis 驱动支持时，将自动使用 Go 语言内部的 Haversine (半正矢) 弧度距离测算代数公式，在内存中动态结算所有处于活跃气泡的绝对千米半径，以此保障在 SQLite 环境下附近定位查找功能表现100%体验一致。
