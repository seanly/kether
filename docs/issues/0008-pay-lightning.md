# 0008 支付接口与 Lightning

**里程碑** M3 · **依赖** 0001

## 目标

定义 `Payer` / `Payee`，并提供一个可打桩的 Lightning 实现。单测不连接真实闪电节点。

## 范围

- `pay`：接口、`PayChallenge` / `PayProof` 的构造与签名
- `request_hash` 的算法见 [design.md](../design.md) 支付一节
- Lightning：`description hash = request_hash`，金额单位 msat
- 内存假节点：`Invoice` 记住 preimage，`Pay` 返回该 preimage，`Verify` 按设计检查

配置项 `MaxPayMsat`：挑战金额更大时 `Payer` 不被调用。

真实 LND 或 Core Lightning 客户端可以留空接口，假节点必须可用。0010 的演示使用假节点或 regtest，由该 issue 选择，本 issue 的 CI 只用假节点。

## 实现要点

每个挑战只接受一次 `Verify`。第二次返回支付无效。

金额非 0 的记录要求 `Invoice` 使用该金额。测试里的发票字符串可以是简化的 BOLT11 形文本，只要 description hash 和 payment hash 的关系成立。

日志不打印 preimage。

## 验收标准

- [x] 正确 preimage 通过 `Verify`，错误 preimage 失败
- [x] 同一挑战第二次 `Verify` 失败
- [x] description hash 与 `request_hash` 不一致时失败
- [x] 金额超过 `MaxPayMsat` 时不调用支付
- [x] 测试不访问网络

## 完成情况

`pay.TestLightningPreimage` 覆盖正确 preimage、错误 preimage、第二次 `Verify` 失败，以及 description hash 与 `request_hash` 不一致。`node.TestPaymentGatesHandler` 在金额超过 `MaxPayMsat` 时不调用 `Payer`，`Handle` 次数保持为 0。测试使用 `MemLightning`，不访问网络，日志不打印 preimage。
