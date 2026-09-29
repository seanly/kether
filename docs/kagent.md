# kagent：公网种子与 NAT 主机

本页是 [设计](design.md) 里「公网可达性」的操作步骤，对应 [0012](issues/0012-nat.md)。`serve` 用 `--public` 声明公网种子，用 `--seed` 拨入；`ask` 用 `--id` 指定要调用的 Agent。

每台机器使用固定的 `--key`。省略 `--key` 时每次启动都会生成新密钥，Peer ID 会变，已经写进 `--seed` 的地址失效。密钥文件权限为 `0600`。

示例里的 `203.0.113.10` 和 `203.0.113.11` 换成公网节点自己的地址。`--echo` 让服务方原样返回正文，演示不需要模型密钥。

成功时标准输出先有一行 `id keth1...`，然后每个已发布地址一行 `addr ...`。地址集合变化后再打印新的 `addr` 行。`id` 是 Agent ID。种子要用整行 `addr`，其中已经包含 `/p2p/<PeerID>`。

## 一台公网种子

公网 IP 配在本机网卡上时，直接监听该地址。打印出的 `addr` 就是 NAT 主机的 `--seed`。

```bash
go run -C cmd/kagent . serve --echo --public --key vps1.key \
  --listen /ip4/203.0.113.10/tcp/4001 \
  --listen /ip4/203.0.113.10/udp/4001/quic-v1
```

防火墙放行 TCP `4001` 和 UDP `4001`。

公网 IP 不在本机网卡上时，监听所有地址，并用 `--announce` 公布公网 IP。未写 `/p2p/` 时，程序补上本机 Peer ID：

```bash
go run -C cmd/kagent . serve --echo --public --key vps1.key \
  --listen /ip4/0.0.0.0/tcp/4001 \
  --listen /ip4/0.0.0.0/udp/4001/quic-v1 \
  --announce /ip4/203.0.113.10/tcp/4001 \
  --announce /ip4/203.0.113.10/udp/4001/quic-v1
```

下面把打印出的 TCP 那一行记为 `<vps1>`。它的形态是：

```text
/ip4/203.0.113.10/tcp/4001/p2p/<VPS1的PeerID>
```

## 第二台公网种子

第二台把第一台写进 `--seed`，并同样标成 `--public`。两台都要长期运行，路由表才是一张。

```bash
go run -C cmd/kagent . serve --echo --public --key vps2.key \
  --seed <vps1> \
  --listen /ip4/203.0.113.11/tcp/4001 \
  --listen /ip4/203.0.113.11/udp/4001/quic-v1
```

把它的 TCP `addr` 行记为 `<vps2>`。只有一台公网机器时，下面的命令只保留 `--seed <vps1>`。

## NAT 上的服务方

服务方不写 `--announce`，也不写 `--public`。监听 TCP 和 QUIC，种子列出每一台公网节点。

```bash
go run -C cmd/kagent . serve --echo --key agent.key \
  --seed <vps1> \
  --seed <vps2> \
  --listen /ip4/0.0.0.0/tcp/0 \
  --listen /ip4/0.0.0.0/udp/0/quic-v1
```

入网后会再打印含 `p2p-circuit` 的 `addr` 行，形态是：

```text
/ip4/203.0.113.10/tcp/4001/p2p/<VPS1的PeerID>/p2p-circuit/p2p/<本机PeerID>
```

等到至少一行这样的地址出现，再从别处调用。同时记下 `id` 行的 `keth1...`，记为 `<agent>`。

## 从另一台 NAT 主机调用

调用方把同一批公网地址当作 `--seed`，并用 `--id` 指定服务方。两台服务都会发布类型 `agent`，不写 `--id` 时 `Search` 可能打到公网种子自己。

```bash
go run -C cmd/kagent . ask --key caller.key \
  --seed <vps1> \
  --seed <vps2> \
  --id <agent> \
  ping
```

`--echo` 的服务方把正文原样返回，标准输出是：

```text
ping
```

流量先经公网种子转发。两边都监听了 QUIC 时，程序会在这条中继连接上尝试打洞。打洞不成，这一次调用仍经种子完成。
