# 0001 项目骨架

**里程碑** M0 · **依赖** 无

## 目标

建立仓库骨架。`go test ./...` 能跑，空包有占位，Makefile 能编译和检查格式。

## 范围

- `go.mod`：module `github.com/seanly/kether`，Go 1.23+
- 目录按 [design.md](../design.md) 的仓库布局建好：`id`、`record`、`dht`、`node`、`session`、`auth`、`pay`、`frame`，各放 `doc.go`
- `Makefile`：`help` / `build` / `test` / `race` / `fmt` / `vet` / `lint` / `tidy` / `clean`
- `.gitignore`：二进制、覆盖率输出
- 根包 `doc.go` 说明库的职责：入网、发现、授权、收款；业务在宿主的处理函数里

本 issue 不实现密钥、DHT、帧或支付。

## 实现要点

`make build` 至少编译 `./...`。还没有 `cmd/twonode` 时，构建成功的标准是包能通过 `go test`，而不是产出演示二进制。演示命令留到 0010。

`make lint` 在未安装 golangci-lint 时可以退化为 `go vet ./...`，并在 help 里写明。

## 验收标准

- [x] `make test` 与 `make vet` 通过
- [x] `gofmt -l .` 无输出
- [x] 上述目录都存在，且每个目录有 `doc.go`
- [x] `go.mod` 的 module 路径是 `github.com/seanly/kether`

## 完成情况

`go.mod` 为 `github.com/seanly/kether`，工具链 Go 1.26.2。`id`、`record`、`dht`、`node`、`session`、`auth`、`pay`、`frame` 均有 `doc.go`。`gofmt -l .` 无输出。`make test`、`make race`、`make vet` 通过。`make lint` 退化为 `go vet ./...`。
