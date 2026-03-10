# proxy-check

`proxy-check` 已重构为一个可部署在服务器上的 HTTP 服务，用于批量检测 `http` 与 `socks5` 代理节点是否可用。

## 功能概览

- 支持 `http://ip:port`
- 支持 `socks5://ip:port`
- 提供 HTTP API 便于外部系统调用
- 支持批量检测
- 支持超时和并发控制
- 返回每个节点的详细检测结果

## 运行环境

- Go `1.22+`
- Linux 服务器（推荐 Ubuntu / Debian / CentOS）
- 可访问外网目标检测地址

## 本地构建

```bash
git clone https://github.com/mmpx12/proxy-check.git
cd proxy-check
go mod tidy
go build -o proxy-check
```

## Docker 部署

### 1. 构建镜像

```bash
docker build -t proxy-check:latest .
```

### 2. 运行容器

```bash
docker run -d --name proxy-check -p 8080:8080 proxy-check:latest
```

### 3. 验证服务

```bash
curl http://127.0.0.1:8080/healthz
```

### 4. 使用 Docker Compose

```bash
docker compose up -d --build
```

验证：

```bash
curl http://127.0.0.1:8080/healthz
```

## 启动服务

```bash
./proxy-check --server --listen 0.0.0.0:8080
```

默认监听地址可自行指定，例如：

```bash
./proxy-check --server --listen 127.0.0.1:8080
```

## 命令行参数

```text
-s, --server         Run as HTTP service
-l, --listen ADDR    HTTP listen address
-v, --version        Print version and exit
```

## HTTP 接口说明

### 1. 健康检查

`GET /healthz`

示例：

```bash
curl http://127.0.0.1:8080/healthz
```

返回：

```json
{
  "ok": true,
  "version": "2.0.0"
}
```

### 2. 检测代理

`POST /check`

请求头：

```text
Content-Type: application/json
```

请求体：

```json
{
  "proxies": [
    "http://34.117.210.247:3128",
    "socks5://34.117.210.247:3128"
  ],
  "target_url": "http://checkip.amazonaws.com",
  "timeout_sec": 5,
  "concurrency": 10
}
```

字段说明：

- `proxies`: 要检测的代理列表，必填
- `target_url`: 用于测试的目标地址，选填，默认 `http://checkip.amazonaws.com`
- `timeout_sec`: 单个代理检测超时秒数，选填，默认 `3`
- `concurrency`: 并发数，选填，默认 `50`

返回示例：

```json
{
  "target_url": "http://checkip.amazonaws.com",
  "total": 2,
  "available": 1,
  "unavailable": 1,
  "results": [
    {
      "proxy": "http://34.117.210.247:3128",
      "available": false,
      "error": "EOF",
      "duration_ms": 1148,
      "proxy_scheme": "http"
    },
    {
      "proxy": "socks5://34.117.210.247:3128",
      "available": true,
      "status_code": 200,
      "duration_ms": 1829,
      "proxy_scheme": "socks5"
    }
  ]
}
```

## 快速测试

### 健康检查

```bash
curl http://127.0.0.1:8080/healthz
```

### 检测单个 IP:Port 的两种协议

```bash
curl -X POST http://127.0.0.1:8080/check \
  -H 'Content-Type: application/json' \
  -d '{
    "proxies": [
      "http://34.117.210.247:3128",
      "socks5://34.117.210.247:3128"
    ],
    "target_url": "http://checkip.amazonaws.com",
    "timeout_sec": 5,
    "concurrency": 2
  }'
```

## 服务器部署全流程

下面以 Linux 服务器为例。

### 1. 安装 Go

确认服务器已安装 Go：

```bash
go version
```

如果未安装，请先按官方方式安装 Go 1.22 或更高版本。

### 2. 拉取项目代码

```bash
git clone https://github.com/mmpx12/proxy-check.git
cd proxy-check
```

### 3. 安装依赖并构建

```bash
go mod tidy
go build -o proxy-check
```

### 4. 创建部署目录

```bash
sudo mkdir -p /opt/proxy-check
sudo cp proxy-check /opt/proxy-check/proxy-check
```

### 5. 创建专用运行用户（推荐）

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin proxycheck
sudo chown -R proxycheck:proxycheck /opt/proxy-check
```

### 6. 编写 systemd 服务文件

创建文件：`/etc/systemd/system/proxy-check.service`

```ini
[Unit]
Description=Proxy Check HTTP Service
After=network.target

[Service]
Type=simple
User=proxycheck
Group=proxycheck
WorkingDirectory=/opt/proxy-check
ExecStart=/opt/proxy-check/proxy-check --server --listen 127.0.0.1:8080
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

### 7. 启动服务

```bash
sudo systemctl daemon-reload
sudo systemctl enable proxy-check
sudo systemctl start proxy-check
```

### 8. 检查服务状态

```bash
sudo systemctl status proxy-check
```

查看日志：

```bash
sudo journalctl -u proxy-check -f
```

### 9. 验证接口

```bash
curl http://127.0.0.1:8080/healthz
```

如果返回 `{"ok":true,...}` 说明服务启动正常。

## 使用 Nginx 对外暴露服务

推荐不要直接把 Go 服务暴露在公网，而是在前面放 Nginx。

### 1. 安装 Nginx

```bash
sudo apt update
sudo apt install -y nginx
```

### 2. 配置反向代理

创建配置文件，例如 `/etc/nginx/conf.d/proxy-check.conf`

```nginx
server {
    listen 80;
    server_name your-domain.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### 3. 检查并重载 Nginx

```bash
sudo nginx -t
sudo systemctl reload nginx
```

### 4. 域名访问测试

```bash
curl http://your-domain.com/healthz
```

## HTTPS 部署（推荐）

如果服务对公网开放，建议开启 HTTPS，可配合 `certbot`：

```bash
sudo apt install -y certbot python3-certbot-nginx
sudo certbot --nginx -d your-domain.com
```

## 安全建议

如果这个服务需要对外开放，建议至少做下面几项：

### 1. 增加鉴权

当前接口默认无鉴权，建议通过 Nginx 增加：

- Basic Auth
- IP 白名单
- Header Token 校验

### 2. 限流

避免被恶意调用导致服务器资源耗尽。

### 3. 控制请求规模

当前服务已限制请求体大小，并限制单次最多检测一定数量的代理。

### 4. 限制 `target_url`

如果业务固定，建议只允许访问白名单目标地址，避免 SSRF 风险。

## 常见问题

### 1. 为什么有些代理超时？

可能原因：

- 代理本身不可用
- 目标网站不可达
- 超时时间过短
- 代理被目标站点拦截

### 2. 为什么同一个 IP:Port，HTTP 不可用但 SOCKS5 可用？

因为同一个端口可能只实现了某一种代理协议，协议不匹配时会失败。

### 3. 默认检测地址是什么？

默认是：

```text
http://checkip.amazonaws.com
```

## 开发与校验

```bash
go test ./...
go build ./...
```

## 版本

```bash
./proxy-check --version
```
