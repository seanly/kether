# AGENTS.md

> Kether — 点对点 Agent 网络的协议和 Go 库。
> 读音与 Zether、Xether 同一型。词义是「冠」。不是 Boneh 等人 2019 年的 Zether 隐私支付协议，也不是 2019 年的 Xether 代币。

**渐进披露：** 本文件是稳定索引。字段、帧和签名域以 [`docs/design.md`](docs/design.md) 为准。可独立交付的切片在 [`docs/issues/`](docs/issues/README.md)，编号即实施顺序。总设计在 `docs/design.md`；每篇 issue 写目标、范围、实现要点、验收标准；完成状态写在 issue 列表的状态列。

## 状态

0001–0014 已完成：身份、签名记录、入网、发现、握手、授权、Lightning 与链上 Bitcoin 打桩支付、调用状态机、`cmd/twonode`、公网可达性（AutoNAT、中继、打洞）、UUID 入网、独立公网中继 `cmd/kether`。

## 快速开始

```bash
make test
make race
make vet
go test ./cmd/twonode
```

公共入口是 `kether.Start`。模块路径 `github.com/seanly/kether`。代码放在本仓库，不放进 `dmr` 或 `bub`。

## 心智模型

1. **启动即入网。** `Start` 用密钥文件里的 UUID 作为 Agent ID，拨配置里的 `host:port` 种子，发布签名记录，并按 TTL 的一半刷新。
2. **发现有两条路。** `Resolve(id)` 按 Agent ID 取最新验签记录。`Search(type)` 只返回 `public` 且声明了该类型的 Agent。
3. **先验证再执行。** `grant_only` 必须带对该 ID 签发的授权。记录标了价就必须先通过支付回执。`Handle` 只在这两步通过之后运行。

传输是 [go-libp2p](https://github.com/libp2p/go-libp2p) 上的私有 Kademlia，协议前缀 `/kether`。Bitcoin 曲线只用于身份。Lightning 与链上 Bitcoin 只用于支付。不要把 Bitcoin 的区块转发网当作消息总线。

## 包

| 包 | 职责 |
|---|---|
| `id` | 密钥、UUID、BIP340。bech32m（HRP `keth`）仍可从公钥导出 |
| `record` | 签名记录的编码与验签 |
| `dht` | DHT 键与 namespace validator |
| `node` | `Start`、发布、搜索、连接、调用 |
| `session` | 流协议 `/kether/1.0.0`。会话类型是 `node.Session` |
| `auth` | 授权签发、校验、进程内次数 |
| `pay` | 支付接口、`MemLightning`、`MemChain` |
| `frame` | 长度前缀帧，上限 1 MiB |
| `cmd/twonode` | 本地双路径演示。库不反向依赖它 |
| `cmd/kether` | 公网中继。监听 `0.0.0.0:4001`，类型 `relay`。库不反向依赖它 |

## 常见任务

| 任务 | 先读 |
|---|---|
| 改字段、错误码、签名域 | [`docs/design.md`](docs/design.md) |
| 看某段是否已交付 | [`docs/issues/README.md`](docs/issues/README.md) 的状态列，再打开对应 issue |
| 嵌入宿主 | 根包 `kether.Start`。业务只注册 `Handle` |
| 本地跑通公开与仅授权 | `cmd/twonode` |
| 公网中继与 NAT 主机 | [`docs/kagent.md`](docs/kagent.md)。中继是 `cmd/kether`，Agent 是 `cmd/kagent` |

## 约定

- 每个改动带测试。Lightning、比特币节点和 libp2p 在单测里用接口打桩或本地 Host。CI 不访问公网，也不要求真实钱包。
- 日志、错误字符串和测试输出里不得出现私钥、种子或 BOLT11 的 preimage。
- 落到磁盘的私钥文件权限为 `0600`。过宽权限拒绝加载。
- `make lint` 即 `go vet ./...`。
- 生成和加载密钥时，若公钥 Y 为奇数则取反私钥。这样 libp2p Peer ID 与 BIP340 x-only 公钥是同一个点。
- 单节点发布时路由表可能为空。记录先落本地，`failed to find any peer in table` 不视为发布失败。
- 新能力沿用 issue 编号续写，不在 issue 里另起一套编码。

## 命令

`make help` 列出 `build`、`test`、`race`、`fmt`、`vet`、`lint`、`tidy`、`clean`。
