# 交给 AI 的部署与使用说明

把这份文档整篇交给执行 AI。目标是部署 [huaqian888/grok2api](https://github.com/huaqian888/grok2api) 的 `server-patches` 分支，使程序行为与参考部署一致，并在使用者给出订阅地址后，按参考部署里的 Resin 节点形态把出口配好。

不要使用仓库 `main`。`main` 是上游新代码，没有这套补丁。不要执行 `docker compose pull`，那会换成官方镜像。

## 完成标准

- 容器 `grok2api` 健康，管理端能用使用者自己的管理员密码登录。
- `config.yaml` 的质量保护、请求重试、停用号复活、新账号宽限、分段选号与下面的「必须保持的配置」一致。
- 密钥、数据库、账号、客户端密钥都是这一套新环境自己的，不是从参考部署复制的。
- 若使用者提供了订阅地址：Build 出口已按 Resin 形态建好，自动分配和自动均衡已打开，质量守护 sidecar 已启动。
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

## 4. 使用者给了订阅地址时，按 Resin 形态配节点

先问清楚这是不是 Resin，或者订阅里的节点用户名是否已经带 `{account}`。没有这句确认时，不要擅自改写用户名。

参考部署的形态是：

- Build 节点全部启用，且是代理池（`proxyPool: true`）。一个地区一条，而不是把整个订阅合成一个节点。容量是这个出口允许挂的账号数，参考里大约是大区几十到 90、小区个位数到十几。
- 代理地址形如 `socks5h://Default.{account}:<密码>@<主机>:<端口>`。`{account}` 让每个账号有稳定的匿名身份，同一账号的 Web、Build、Console 可以共用。
- 需要走 Web 的地区，再做一份 `grok_web` 代理池，容量与对应 Build 节点相同。
- 另外留一个不进代理池的 Web 节点，作为 Web 固定回退。
- Build 回退模式是 `none`。Web 回退模式是 `fixed`，指向那个不进池的备用节点。Console 没有单独节点时保持 `none`，不要把 Console 回退指到别的范围。
- 连通性探测用 `cloudflare`，间隔 900 秒。自动分配和自动均衡都打开，分配间隔 300 秒。

管理 API 前缀是 `/api/admin/v1`，先用管理员登录拿到会话，再带登录态调用。下面的 JSON 里，订阅地址和密码只用使用者刚刚提供的值。

### 4.1 订阅源导入

每个范围单独建一个源。Build：

```http
POST /api/admin/v1/egress-sources
Content-Type: application/json

{
  "name": "build-subscription",
  "scope": "grok_build",
  "enabled": true,
  "url": "<使用者给的订阅地址>",
  "refreshIntervalSeconds": 900,
  "defaultAccountCapacity": 15
}
```

返回的 `id` 接着同步：

```http
POST /api/admin/v1/egress-sources/<id>/sync
```

Web 若也要走这些出口，再用同一地址建一个 `scope` 为 `grok_web` 的源并同步。`defaultAccountCapacity` 先给一个正数，同步后按地区改。

然后 `GET /api/admin/v1/egress-nodes`，对每个新建的 Build 和 Web 池节点：

```http
PUT /api/admin/v1/egress-nodes/<id>
Content-Type: application/json

{
  "name": "<保持或按地区改写的名字>",
  "scope": "grok_build",
  "enabled": true,
  "proxyPool": true,
  "accountCapacity": 15
}
```

`scope` 必须是该节点原来的范围。导入出来的节点默认不是代理池，这一步必须补上，否则和参考部署的 Resin 池不一样。

### 4.2 确认是 Resin 时改用户名

只有使用者确认这是 Resin，或链接里已经有 `{account}` 时，才把每条代理改成：

```text
socks5h://Default.{account}:<原密码>@<原主机>:<原端口>
```

密码、主机、端口保持订阅里的值，不要自己生成密码，也不要把改完的 URL 打印到聊天记录。用节点更新接口的 `proxyURL` 字段写回。一个地区留一条池节点即可；同一网关只是地区不同时，按地区拆开并分别设容量。

普通机场订阅不要改用户名。保持订阅原文，只补 `proxyPool: true` 和容量。

### 4.3 Web 固定回退

选一个稳定地区，再建一个不进代理池的 Web 节点，例如名字带 `Web Fallback`。`proxyPool` 为 false，`accountCapacity` 可以较大。它使用和对应池节点相同的代理地址。

然后打开自动分配，并把回退设成参考形态：

```http
PUT /api/admin/v1/egress-operations
Content-Type: application/json

{
  "probeProvider": "cloudflare",
  "probeIntervalSeconds": 900,
  "autoAssignEnabled": true,
  "autoBalanceEnabled": true,
  "assignmentIntervalSeconds": 300,
  "fallbacks": {
    "grok_build": {"mode": "none", "nodeId": ""},
    "grok_web": {"mode": "fixed", "nodeId": "<Web Fallback 的节点 ID>"},
    "grok_console": {"mode": "none", "nodeId": ""},
    "grok_web_asset": {"mode": "none", "nodeId": ""},
    "grok_console_asset": {"mode": "none", "nodeId": ""}
  }
}
```

固定回退节点要保持启用。质量守护会把它标成受保护，不参与自动隔离。

### 4.4 探测

```http
POST /api/admin/v1/egress-nodes/test
Content-Type: application/json

{"ids": []}
```

`ids` 为空时测试当前有代理的节点。不健康的节点不要分配账号；把错误告诉使用者，不要为了数量去启用明显连不上的节点。

最后确认质量守护 sidecar 在跑，且 `qualityGuard.nodeIDs` 仍是空数组。有 Build 账号之后，在质量守护页对一个节点做一次检测，确认不是「没有可调度账号」。

## 5. 汇报

告诉使用者：

- 管理端地址和管理员用户名。密码只在当时设置时出现过一次，不要再次贴出。
- 镜像是本地构建的 `grok2api:local`，分支是 `server-patches`。
- 质量守护 sidecar 是否已启动。
- 若配了订阅：Build / Web 各多少节点、是否为代理池、Web 固定回退节点名字、探测健康数。不要附带代理 URL。
- 还需要他们自己导入 Build 账号，并创建一个客户端密钥后才能调用。
