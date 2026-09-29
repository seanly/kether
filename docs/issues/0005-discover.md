# 0005 发现：Resolve 与 Search

**里程碑** M2 · **依赖** 0004

## 目标

调用方可以按 Agent ID 取到最新有效记录，或按类型列出公开 Agent。

## 范围

- `Resolve(id)` 与 `Search(type)`，步骤见 [design.md](../design.md) 发现一节
- 验签失败、过期、`peer_id` 不匹配的记录不返回给调用方

## 实现要点

`Search` 对 provider 里的每个 ID 再 `Resolve`。丢掉 `access != public` 的，以及类型列表里没有该标签的。结果按 `seq` 从新到旧。

测试网络仍是本地两节点或三节点。制造一条签名错误的记录放进 DHT，确认 `Resolve` 返回错误而不是地址。

## 验收标准

- [x] `Resolve` 返回验签通过的最新记录
- [x] 伪造签名的记录被拒绝
- [x] `Search("echo")` 只返回公开且声明了 `echo` 的 Agent
- [x] `grant_only` 的 Agent 可以被 `Resolve`，`Search` 结果里没有它
- [x] 同一 ID 的旧 `seq` 不会盖过新记录

## 完成情况

`TestDiscoverAndInvoke` 检查 `Resolve` 验签、伪造记录被 DHT validator 拒绝，以及 `Search("echo")`。`TestGrantOnly` 能按 ID 解析，搜索结果里没有该 Agent。同一 ID 的旧 `seq` 由 `record.TestPreferSeq` 与 `dht.TestValidator` 保证不覆盖新记录。
