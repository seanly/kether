# 0006 帧、握手与连接

**里程碑** M3 · **依赖** 0004

## 目标

按记录里的地址拨号，完成双向身份握手，并在流上收发长度前缀帧。

## 范围

- 协议 `/kether/1.0.0`
- `Envelope` 及 `Hello` / `HelloAck` / `Error`，帧格式见 [design.md](../design.md) 连接与帧一节
- `Connect`：`Resolve`、拨号、握手、核对公钥与 Peer ID
- 长度上限 1 MiB

本 issue 的流在握手成功后可以发一条测试帧并收回。`Invoke` 状态机在 0009。

## 实现要点

服务方先发 `Hello`（含 32 字节随机数）。调用方回复 `Hello` 和对服务方随机数的签名。服务方发 `HelloAck`。签名域是 `kether/hello/v1`。

对端公钥、Agent ID、Peer ID 必须与用来拨号的记录一致，否则发 `Error` 并关流。超过 1 MiB 的帧关流。

后续调用复用已握手的流，每条使用新的 `corr_id`。复用的测试可以只检查第二次消息不再走 Hello。

## 验收标准

- [x] 两节点握手成功，双方看到的对端 Agent ID 正确
- [x] 记录中的 Peer ID 与实际连接不一致时握手失败
- [x] 错误的签名被拒绝
- [x] 超过 1 MiB 的帧导致流关闭
- [x] 握手后的第二条消息沿用同一条流

## 完成情况

`TestDiscoverAndInvoke` 两次 `Invoke` 走同一条已握手的流。`TestBadHello` 拒绝错误签名。`TestPeerMismatch` 在声称的 Agent ID 与连接 Peer ID 不一致时失败。超过 1 MiB 的帧由 `frame.TestTooLarge` 关闭读取。
