# 0010 双进程演示

**里程碑** M3 · **依赖** 0005, 0009

## 目标

两个进程在本机走完设计文档里的公开路径和授权路径。

## 范围

- `cmd/twonode`：启动种子节点 A 与客户端 B
- 支付使用 0008 的假 Lightning，或本机 regtest。CI 与默认 `make test` 使用假 Lightning
- 路径见 [design.md](../design.md) 最小实现切片

## 实现要点

A 先监听并打印自己的 Agent ID 与 multiaddr。B 用该地址作种子。

公开模式：A 的类型是 `echo`，价格 1000 msat。B `Search("echo")` 后 `Invoke`，stdout 打印回显。

授权模式：A 为 `grant_only`。B 的搜索结果为空。无授权的调用得到错误码 `2`。A 为 B 的 ID 签发一小时授权后，同一次演示里的第二次调用成功。

演示进程的退出码：两条路径都成功才为 0。

## 验收标准

- [x] `go test` 覆盖上述两条路径，使用假 Lightning
- [x] 授权路径下 `Search` 为空，无授权调用失败，有授权调用回显
- [x] 演示日志不含私钥和 preimage

## 完成情况

`cmd/twonode.TestTwoNode` 在一次进程里跑完两条路径：A 公开 `echo` 并收取 1000 msat 后回显；A 改为仅授权后 B 搜不到，直到 A 对 B 的 ID 签发授权。输出含 `public`、`grant` 与 `ok`，不含 preimage。支付使用 `MemLightning`。
