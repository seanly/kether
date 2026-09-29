# 0004 入网与私有 DHT

**里程碑** M2 · **依赖** 0003

## 目标

`Start` 之后节点连上种子，加入私有 Kademlia，并周期发布自己的签名记录。

## 范围

- `node.Start` / `Close`：libp2p Host、协议 `/kether/kad/1.0.0`、种子拨号、路由引导、记录刷新
- DHT 只在 Kether 节点之间使用
- 单值上限 8 KiB，超出则拒绝发布
- 键 `/kether/agent/1/<agent-id>`。`access = public` 时再为每个类型写 provider；`grant_only` 不写类型键

类型搜索的读取放在 0005。本 issue 只保证写入后，同一 DHT 上的另一个节点能按 agent 键读回并验签。

## 实现要点

种子一个都拨不通时 `Start` 返回错误。刷新周期是 `ExpiresAt` 剩余时间的一半，刷新时递增 `seq`。

`Close` 停止刷新并关闭 Host。测试用两个本地 Host，不连公网。

## 验收标准

- [x] 无可用种子时 `Start` 失败
- [x] B 以 A 为种子启动后，能读到 A 的记录且验签通过
- [x] `grant_only` 的 A 不出现在类型 provider 里
- [x] 超过 8 KiB 的记录发布失败
- [x] `Close` 之后刷新协程退出

## 完成情况

`node.TestUnreachableSeed`、`TestRecordTooBig`、`TestDiscoverAndInvoke`、`TestGrantOnly`、`TestCloseStopsRefresh` 覆盖验收项。种子列表为空表示本机就是种子。路由表为空时记录先写入本地，`failed to find any peer in table` 不视为发布失败。测试使用本地 Host，不连公网。
