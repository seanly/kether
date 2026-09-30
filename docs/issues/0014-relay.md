# 0014 独立的公网中继

**里程碑** M5 · **依赖** 0013

## 目标

公网种子、AutoNAT 回拨和 circuit 中继由 `cmd/kether` 单独运行。它加入同一张私有 DHT，不回答类型 `agent` 的调用。NAT 上的 Agent 仍用 `kagent`，种子只填中继的 `host:port`。

## 范围

- `cmd/kether`：`ReachabilityPublic`，监听 `0.0.0.0:4001` 的 TCP 和 QUIC，记录类型 `relay`，不注册 `Handle`
- `--key` 与 `kagent` 相同：缺文件则生成，权限 `0600`。省略时每次启动都是新 Peer ID
- 重复 `--seed host:port` 拨其他中继。多台中继不会自动互相发现
- 标准输出一行 `listen 0.0.0.0:4001`
- `kagent serve` 去掉 `--public`

库的可达性选项不变。打洞仍在 NAT 侧打开。

## 实现要点

`cmd/kether` 在根模块里，调用 `kether.Start`。空 `--seed` 的第一台自己成为种子。后一台至少拨一台已经在线的中继，连接是双向的。

类型是 `relay` 且 `AccessPublic`，所以 `Search("agent")` 不会返回它。`Search("relay")` 可以找到它。没有处理函数时，`Invoke` 走现有的失败路径。

NAT 节点把每一台中继写入 `--seed`。预约数仍是 `min(种子数, 2)`。

## 验收标准

- [x] `cmd/kether` 用临时端口启动，第二台用 `host:port` 拨入
- [x] 两个节点经这台中继入网后，`Search("agent")` 不含中继，`Search("relay")` 含中继且类型为 `relay`
- [x] 标准输出是 `listen 0.0.0.0:4001`，没有地址行
- [x] `kagent serve` 不再接受 `--public`
- [x] 本地测试不访问公网，也不绑定 4001

## 完成情况

`TestRelayIsNotAnAgent` 起一台中继、第二台中继和两个节点，核对类型搜索。`TestWriteReady` 核对输出。`TestServeSeedAndAskID` 改为普通 `kagent` 种子。`TestRelayHandshake` 仍覆盖经中继握手。
