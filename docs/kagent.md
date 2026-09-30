# kagent：一个 UUID 入网

本页是 [设计](design.md) 里身份和「公网可达性」的操作步骤，对应 [0014](issues/0014-relay.md)。公网中继是 [`cmd/kether`](../cmd/kether)。人要保存和传递的是 Agent 的 UUID。种子只配置中继的 `host:port`。

每台机器使用固定的 `--key`。省略 `--key` 时每次启动都会生成新密钥和新 UUID。密钥文件权限为 `0600`，里面是 32 字节私钥和 16 字节 UUID。

示例里的 `203.0.113.10` 换成公网中继自己的地址。`--echo` 让服务方原样返回正文，演示不需要模型密钥。

`kagent serve` 成功时标准输出只有一行：

```text
uuid 01234567-89ab-4cde-8fab-0123456789ab
```

## 一台公网中继

`kether` 默认监听 `0.0.0.0:4001` 的 TCP 和 QUIC，打开 AutoNAT 回拨和 circuit 中继。防火墙放行这两个端口。标准输出一行 `listen 0.0.0.0:4001`。

```bash
go run ./cmd/kether --key vps.key
```

别的机器入网时，`--seed` 只写这台机器的 `host:port`，例如 `203.0.113.10:4001`。

## 第二台公网中继

多台中继不会自动互相发现。第二台把第一台写进 `--seed`。拨通之后两边在同一张 DHT 里。再加一台时，`--seed` 指向其中任意一台即可。

```bash
go run ./cmd/kether --key vps2.key --seed 203.0.113.10:4001
```

只有一台公网机器时，下面的命令只保留一个 `--seed`。NAT 节点把每一台中继都写进 `--seed`。

## NAT 上的服务方

`kagent` 不承担中继。程序自己监听临时端口上的 TCP 和 QUIC。种子列出每一台公网中继。

```bash
go run -C cmd/kagent . serve --echo --key agent.key --seed 203.0.113.10:4001
```

把打印出的那一行 UUID 记为 `<uuid>`。中继地址留在节点内部，不出现在输出里。中继发布的类型是 `relay`，`Search("agent")` 不会选中它。

## 从另一台 NAT 主机调用

`--id` 是对方的 UUID。

```bash
go run -C cmd/kagent . ask --key caller.key --seed 203.0.113.10:4001 --id <uuid> ping
```

`--echo` 的服务方把正文原样返回，标准输出是：

```text
ping
```

流量先经公网中继转发。两边都监听了 QUIC 时，程序会在这条中继连接上尝试打洞。打洞不成，这一次调用仍经中继完成。
