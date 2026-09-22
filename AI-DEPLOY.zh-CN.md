# 交给 AI 的部署与使用说明

把这份文档整篇交给执行 AI。目标是部署 [huaqian888/grok2api](https://github.com/huaqian888/grok2api) 的 `server-patches` 分支，使程序行为与参考部署一致，并在使用者给出订阅地址后，按参考部署里的 Resin 节点形态把出口配好。

不要使用仓库 `main`。`main` 是上游新代码，没有这套补丁。不要执行 `docker compose pull`，那会换成官方镜像。

## 完成标准

- 容器 `grok2api` 健康，管理端能用使用者自己的管理员密码登录。
- `config.yaml` 的质量保护、请求重试、停用号复活、新账号宽限、分段选号与下面的「必须保持的配置」一致。
- 密钥、数据库、账号、客户端密钥都是这一套新环境自己的，不是从参考部署复制的。
- 若使用者提供了订阅地址：订阅已登记在 Resin 里，并按地区建成平台；grok2api 里每个平台只有一条 `socks5h://平台名.{account}@resin:2260` 的 Build 代理池入口。没有把订阅导入 grok2api。
- 向使用者说明管理端地址、管理员账号，以及客户端密钥要在登录后创建。不要在回复里重复代理密码、订阅地址全文或加密密钥。

## 禁止

- 不要把参考部署的 `config.yaml`、`.env`、数据库、JWT、加密密钥、客户端密钥或代理密码抄过来。加密密钥一旦和别人的数据库混用，账号凭据无法解密。
- 不要把订阅地址、代理 URL、管理员密码写进 git。
- 不要为了「配得一样」去关闭 `requestRetry` 或 `disabledRevival`。参考部署这两项是开着的。
- 不要把 Build 的固定回退设到某一个节点。参考部署的 Build 回退是 `none`，这样质量守护才能隔离有问题的出口。

## 1. 取代码并构建

```bash
git clone https://github.com/huaqian888/grok2api.git
cd grok2api
git checkout server-patches
cp config.example.yaml config.yaml
openssl rand -hex 32
openssl rand -base64 32
```

把第一行随机值写入 `secrets.jwtSecret`，第二行写入 `secrets.credentialEncryptionKey`。给 `bootstrapAdmin.password` 设一个强密码，`bootstrapAdmin.username` 可保持 `admin`。其余配置项保持 `config.example.yaml` 的值。

`config.example.yaml` 已经是参考部署的运行参数。下面这些值必须保持，不要改回模板旧值：

| 项 | 值 |
|---|---|
| `qualityGuard.enabled` | `true` |
| `qualityGuard.model` | `grok-4.6` |
| `qualityGuard.mode` | `hybrid` |
| `qualityGuard.failClosed` | `false` |
| `qualityGuard.minimumHealthyNodes` | `3` |
| `qualityGuard.nodeIDs` | `[]`（空表示管理全部 Build 节点） |
| `qualityGuard.requestRetry.enabled` | `true` |
| `qualityGuard.requestRetry.maxAttempts` | `6` |
| `qualityGuard.requestRetry.holdTimeout` | `30s` |
| `qualityGuard.requestRetry.minOutputTokens` | `8` |
| `qualityGuard.requestRetry.onExhausted` | `fail_closed` |
| `qualityGuard.requestRetry.accountCooldown` | `12h` |
| `qualityGuard.requestRetry.idleAccountCooldown` | `15m` |
| `qualityGuard.requestRetry.newAccountGrace` | `0s`（写 `0s` 表示不给新账号宽限；删掉这一项才会用程序默认的 12 小时） |
| `qualityGuard.requestRetry.disabledRevival.enabled` | `true` |
| `qualityGuard.requestRetry.disabledRevival.batchSize` | `100` |
| `qualityGuard.requestRetry.disabledRevival.concurrency` | `32` |
| `qualityGuard.requestRetry.disabledRevival.timeout` | `120s` |
| `qualityGuard.requestRetry.disabledRevival.failRetryAfter` | `6h` |
| `routing.segmentedSelectorEnabled` | `false` |
| `database.driver` | `sqlite` |
| `runtimeStore.driver` | `memory` |

仓库说明里如果还写着「`requestRetry.enabled` 仍为 false」，以本文件和 `config.example.yaml` 为准。

主程序 compose 只有镜像名，没有 build。必须先在本地打镜像：

```bash
docker build -t grok2api:local .
printf 'GROK2API_IMAGE=grok2api:local\n' > .env
docker compose up -d
```

容器里的监听地址会被启动参数改成 `0.0.0.0:8000`。宿主机端口默认 `8000`，可用环境变量 `GROK2API_PORT` 改。

自检：

```bash
docker compose ps
curl -fsS http://127.0.0.1:8000/healthz
```

`/healthz` 返回成功后，用浏览器打开 `http://<主机>:8000`，用刚才的管理员账号登录。

## 2. 教使用者怎么用

登录后按这个顺序做，并在做完时把对应菜单位置告诉使用者。

1. **账号。** 在账号页导入他们自己的 Grok Build 账号。这套仓库不包含参考部署的号池。没有账号时，模型和质量探测都不会有结果。
2. **模型路由。** 路由页可以搜 `grok-4.6` 和 `grok-4.7`。点「同步模型」时，新目录里出现的模型会加上；这个分支已经改成不会删掉该账号原来已经记下的模型。所以同步之后，原来能用的 `grok-4.6` 应继续显示为支持。支持数是「当前目录快照里有这个模型的启用账号」，不是「曾经用通过」。
3. **客户端密钥。** 在客户端密钥页创建一个 Key。调用接口时用 `Authorization: Bearer <key>`。不要把管理员密码当作接口密钥。
4. **调用。** 兼容 OpenAI 风格的接口在 `/v1/*`。文本示例：

```bash
curl http://127.0.0.1:8000/v1/responses \
  -H "Authorization: Bearer <客户端密钥>" \
  -H "Content-Type: application/json" \
  -d '{"model":"grok-4.6","input":"用三句话解释量子隧穿。","stream":true}'
```

对外模型名以管理端模型路由里的「对外模型名称」为准。`grok-4.6` 在这套配置里会走 Build，并接受质量重试。

5. **质量不好时程序会怎么做。** `requestRetry` 开着：思考模型若输出够了 `minOutputTokens` 却没有思考内容，这次结果不会直接发给用户，会换账号再试，最多 `maxAttempts` 次。耗尽后 `fail_closed` 返回 503，而不是把降智正文交出去。第一次判定缺思考会冷却账号 12 小时；冷却结束后再缺，才会停用。空流的空闲惩罚是 15 分钟，和新账号宽限无关。`newAccountGrace: 0s` 表示新账号没有额外宽限。
6. **停用号。** 定时任务会按 `disabledRevival` 探测「缺思考」和「思考试用」的停用 Build 账号，仍能思考的会重新启用。超时不会当成确认缺思考。管理端账号页的「探测并启用」用的是同一套规则。探测超时是 120 秒。
7. **审计。** 请求审计页可以按时间范围筛选，用来看 403、空流、质量拦截。

## 3. 质量守护怎么开

质量守护分两层，都要有：

- **请求路径**已经写在 `config.yaml` 的 `qualityGuard.requestRetry` 里，主程序启动后就生效，不需要 sidecar。
- **出口节点隔离**是 sidecar。它读真实请求的速度，也会主动发探测。只执行 `docker compose up -d` 不会启动它。

在主程序健康之后执行：

```bash
docker compose --profile quality-guard up -d --build
```

改 `config.yaml` 里的守护基础项后，执行：

```bash
docker compose --profile quality-guard restart grok2api egress-quality-guard
```

管理端「质量守护」页改策略可以热加载，不用为了页面上的阈值重启。

参考策略就是 `config.example.yaml` 里的混合模式：

- 每 5 秒看一次真实请求审计。
- 硬阈值 1000 Token/s 直接隔离；软阈值 500 Token/s 要连续确认。
- 主动探测间隔 30 分钟。
- 至少留 3 个健康节点，低于这个数就不再继续隔离。
- `failClosed` 保持 false。
- `nodeIDs` 保持空，管全部 Build 节点。被设成固定回退的节点受保护，不会被自动隔离。
- 用户名里带 `{account}` 的节点不会整节点停用。异常只摘掉对应账号在这个节点上的租约，冷却后用同一账号和同一节点复测。

管理端左侧「质量守护」用来看每个节点的 Token/s、首字延迟、隔离和恢复。可以对单个节点做一次真实模型检测。检测需要至少有一个能调度的 Build 账号。

## 4. 使用者给了订阅地址时，按现有 Resin 方案配节点

不要把订阅导入 grok2api 的订阅源，也不要调用 `/api/admin/v1/egress-sources`。参考部署没有使用这个功能。订阅只登记到旁边的 Resin 网关；grok2api 里每个地区只留一条指向 Resin 平台的入口。

### Resin 本身

参考机器上 Resin 是独立容器，不是 grok2api 的一个菜单：

- 镜像 `ghcr.io/resinat/resin:1.2.0`，容器名 `grok2api-resin`。
- 加入 Docker 网络 `grok2api_default`，网络别名是 `resin`，所以 grok2api 容器里用主机名 `resin` 访问它。
- 端口只发布在宿主机 `127.0.0.1:2260`，容器内也是 `2260`。
- 管理接口是 `http://127.0.0.1:2260/api/v1/`，请求头 `Authorization: Bearer <RESIN_ADMIN_TOKEN>`。
- grok2api 连接它时用的密码是另一份 `RESIN_PROXY_TOKEN`。两个令牌都放在这份 Resin 自己的环境文件里，不要抄参考机上的值。
- 健康检查是 `http://127.0.0.1:2260/healthz`。

新机器上还没有 Resin 时，按上面的镜像、网络和端口起一份。两个令牌用 `openssl rand -hex 32` 各自生成。Resin 没健康之前，不要创建 grok2api 节点。

### 4.1 把使用者给的订阅登记进 Resin

向使用者要两样东西：订阅地址，以及一个短名称，例如 `Rockey`。短名称会变成节点名前缀，用字母和数字，不要带空格。

已有同名订阅就只刷新，不要再建一条：

```http
POST http://127.0.0.1:2260/api/v1/subscriptions
Authorization: Bearer <RESIN_ADMIN_TOKEN>
Content-Type: application/json

{
  "name": "<短名称>",
  "source_type": "remote",
  "url": "<使用者给的订阅地址>",
  "update_interval": "1h",
  "enabled": true,
  "ephemeral": false
}
```

然后：

```http
POST http://127.0.0.1:2260/api/v1/subscriptions/<id>/actions/refresh
```

轮询 `GET /api/v1/subscriptions?limit=1000&offset=0`，直到这条订阅有 `last_updated` 或 `last_error`。有错误就停下来告诉使用者，不要继续建平台和 grok 节点。不要把接口返回体贴到聊天里，里面可能含订阅地址。

### 4.2 按地区做成 Resin 平台

```http
GET http://127.0.0.1:2260/api/v1/nodes?limit=100000&offset=0
```

只使用同时满足这些条件的节点：`enabled`、`has_outbound`、没有 `circuit_open_since`、有 `egress_ip`，并且 `tags` 里的 `subscription_name` 等于刚才的短名称。

按 `region` 分组，规则和参考部署一样：

- 同一订阅、同一地区一组。平台名是 `<短名称>-<地区大写>`，例如 `Rockey-TW`。
- 节点太少、不值得单独成组的地区并进 `<短名称>-Other`。
- 一组里不同的 `egress_ip` 少于 2 个就跳过，不建平台。
- 账号容量 = 不同出口 IP 的数量 × 3。

新建或更新平台：

```http
POST http://127.0.0.1:2260/api/v1/platforms
Content-Type: application/json

{
  "name": "Rockey-TW",
  "sticky_ttl": "168h",
  "regex_filters": ["^Rockey/"],
  "region_filters": ["tw"],
  "allocation_policy": "PREFER_IDLE_IP",
  "passive_circuit_breaker_disabled": false
}
```

`regex_filters` 用 `^<短名称>/`。同名平台已存在时改用 `PATCH /api/v1/platforms/<id>`，不要改名字。`Other` 的 `region_filters` 写成实际并进去的那些地区。

### 4.3 grok2api 只建平台入口

不要为订阅里的每一个代理各建一个 grok2api 节点。每个 Resin 平台只建一条 Build 节点：

```http
POST /api/admin/v1/egress-nodes
Content-Type: application/json

{
  "name": "Resin Rockey-TW",
  "scope": "grok_build",
  "enabled": true,
  "proxyPool": true,
  "proxyURL": "socks5h://Rockey-TW.{account}:<RESIN_PROXY_TOKEN>@resin:2260",
  "accountCapacity": 15,
  "userAgent": "",
  "cloudflareCookies": ""
}
```

这里的用户名是 `平台名.{account}`，主机固定是 `resin`，端口固定是 `2260`，密码是这份部署自己的 `RESIN_PROXY_TOKEN`。`{account}` 必须原样保留。`accountCapacity` 用上一节算出来的容量，不要一律写死。

同名节点已存在就 `PUT /api/admin/v1/egress-nodes/<id>`。返回里 `proxyPool`、`accountBoundProxy`、`proxyConfigured` 必须都为真，否则停下来查地址是否写成了上面的形式。

Build 回退保持 `none`。自动分配和自动均衡打开，探测用 `cloudflare`、间隔 900 秒、分配间隔 300 秒。这些节点带 `{account}`，质量守护只会摘掉对应账号的租约，不会把整条平台入口停用。

### 4.4 Web

参考部署里，部分平台另外有同名的 `grok_web` 代理池，以及一个不进代理池的 Web 固定回退。使用者没有要求 Web 时，不要自动做这一步。使用者要求时，用同一条 `socks5h://平台名.{account}:<RESIN_PROXY_TOKEN>@resin:2260` 再建 `scope` 为 `grok_web` 的代理池，并单独留一个 `proxyPool: false` 的 Web 节点作为 `grok_web` 的 `fixed` 回退。Console 没有单独平台时，回退保持 `none`。

### 4.5 不要做的事

- 不要调用 `/api/admin/v1/egress-sources`，也不要把订阅地址写进 grok2api。
- 不要把 Resin 拉下来的具体代理逐条导入 grok2api。
- 不要把用户名改成 `Default.{account}`。用户名是平台名，例如 `Rockey-TW.{account}`。
- 不要在聊天、日志或 git 里写出订阅地址、`RESIN_ADMIN_TOKEN`、`RESIN_PROXY_TOKEN`。

## 5. 汇报

告诉使用者：

- 管理端地址和管理员用户名。密码只在当时设置时出现过一次，不要再次贴出。
- 镜像是本地构建的 `grok2api:local`，分支是 `server-patches`。
- 质量守护 sidecar 是否已启动。
- 若配了订阅：Build / Web 各多少节点、是否为代理池、Web 固定回退节点名字、探测健康数。不要附带代理 URL。
- 还需要他们自己导入 Build 账号，并创建一个客户端密钥后才能调用。
