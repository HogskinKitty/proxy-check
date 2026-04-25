# proxy-check

轻量级 HTTP 代理检测服务，批量验证 HTTP/SOCKS5 代理可用性。

## 快速开始

```bash
# Docker
docker run -d -p 8080:8080 ghcr.io/hogskinkitty/proxy-check:latest

# 或从源码构建
go build -o proxy-check . && ./proxy-check --server
```

```bash
curl http://127.0.0.1:8080/healthz
# {"ok":true,"version":"2.0.0"}
```

## API

### `GET /healthz`

健康检查，无需认证。

### `POST /check`

检测代理可用性。

```bash
curl -X POST http://127.0.0.1:8080/check \
  -H 'Content-Type: application/json' \
  -d '{
    "proxies": [
      "http://1.2.3.4:8080",
      "socks5://1.2.3.4:1080"
    ]
  }'
```

**请求字段：**

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `proxies` | `[]string` | 是 | — | 代理列表，上限 1000 |
| `target_url` | `string` | 否 | `http://checkip.amazonaws.com` | 检测目标地址 |
| `timeout_sec` | `int` | 否 | `3` | 单个代理超时秒数 |
| `concurrency` | `int` | 否 | `50` | 并发数 |

**响应示例：**

```json
{
  "target_url": "http://checkip.amazonaws.com",
  "total": 2,
  "available": 1,
  "unavailable": 1,
  "results": [
    {
      "proxy": "http://1.2.3.4:8080",
      "available": false,
      "error": "EOF",
      "duration_ms": 1148,
      "proxy_scheme": "http"
    },
    {
      "proxy": "socks5://1.2.3.4:1080",
      "available": true,
      "status_code": 200,
      "duration_ms": 1829,
      "proxy_scheme": "socks5"
    }
  ]
}
```

## 认证

通过环境变量 `API_KEY` 启用 API Key 认证，未配置时无需认证。

```bash
# 单个 Key
docker run -d -p 8080:8080 -e API_KEY=my-secret-key ghcr.io/hogskinkitty/proxy-check:latest

# 多个 Key（逗号分隔）
docker run -d -p 8080:8080 -e API_KEY=key1,key2,key3 ghcr.io/hogskinkitty/proxy-check:latest

# 从文件加载（适用于 Docker Secret）
docker run -d -p 8080:8080 -e API_KEY_FILE=/run/secrets/api_key ghcr.io/hogskinkitty/proxy-check:latest
```

请求时通过 Header 传递 Key（二选一）：

```bash
# Bearer Token
curl -H "Authorization: Bearer my-secret-key" http://127.0.0.1:8080/check -d '...'

# X-API-Key
curl -H "X-API-Key: my-secret-key" http://127.0.0.1:8080/check -d '...'
```

`/healthz` 始终免认证。

## 命令行参数

```text
-s, --server         Run as HTTP service
-l, --listen ADDR    HTTP listen address (default :8080)
-v, --version        Print version and exit
```

## 部署

### Docker Compose

```bash
docker compose up -d
```

### systemd

```bash
go build -o proxy-check .
sudo cp proxy-check /usr/local/bin/
```

创建 `/etc/systemd/system/proxy-check.service`：

```ini
[Unit]
Description=Proxy Check Service
After=network.target

[Service]
ExecStart=/usr/local/bin/proxy-check --server --listen 127.0.0.1:8080
Restart=always
Environment=API_KEY=your-key-here

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now proxy-check
```

## 安全建议

- 对公网开放时务必启用 `API_KEY` 认证
- 限制 `target_url` 白名单，避免 SSRF 风险
