# 🚀 HyperTimerWheel

一个面向游戏服务器的高性能多层时间轮，使用 Go 编写。

它适合承载技能 CD、定时邮件、改名冷却、状态过期、延迟通知等大量与时间相关的任务。

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)
![Status](https://img.shields.io/badge/status-learning%20%2F%20developing-orange)

## ✨ 项目简介

HyperTimerWheel 使用分层时间轮降低大量定时器场景下的调度成本：

- 低层提供较高的时间精度；
- 高层覆盖更长的时间范围；
- 时间推进由服务器自己的 tick 驱动；
- `Schedule` 和 `Reset` 通过命令队列提交，避免业务 goroutine 直接修改时间轮内部结构；
- callback 可以交给可选的 worker pool 异步执行；
- 当前提供 slice + generation 和 linked-list 两种桶实现。

当前项目仍处于学习和持续完善阶段，API 和内部实现可能继续调整。

## 🏁 快速开始

```text


start := time.Now()

wheel, err := timerwheel.NewWheel(timerwheel.WheelConfig{
    BaseTick:      10 * time.Millisecond,
    MaxDelay:      time.Minute,
    SlotsPerLevel: 64,
    SlotType:      timerwheel.SlotTypeSlice,
    StartTime:     start,
    WorkerPoolConfig: timerwheel.WorkerPoolConfig{
        WorkerNum:  4,
        QueueSize:  1024,
    },
})
if err != nil {
    panic(err)
}
defer wheel.Close()

_, err = wheel.Schedule(500*time.Millisecond, func (now time.Time) {
    fmt.Println("timer fired at", now)
})
if err != nil {
    panic(err)
}

// 在游戏服务器主循环中推进时间轮。
for {
    time.Sleep(10 * time.Millisecond)
    wheel.Advance(time.Now())
}

```

如果不设置 `StartTime`，时间轮会默认使用创建时的 `time.Now()`。

## 🧰 核心 API

| API        | 作用                          |
|------------|-----------------------------|
| `NewWheel` | 创建时间轮                       |
| `Schedule` | 添加一个延迟任务                    |
| `Reset`    | 修改已有 timer 的延迟              |
| `Cancel`   | 取消 timer                    |
| `Advance`  | 推进逻辑时间并触发到期 timer           |
| `Close`    | 停止时间轮并等待 worker pool 中的任务结束 |

注意：`Advance` 应由一个固定的服务器 tick goroutine 调用。`Schedule`、`Reset` 和 `Cancel` 可以由业务 goroutine 调用。调用
`Close` 前，应先停止新的 `Schedule`/`Reset` 提交并等待提交 goroutine 结束。

## 🧱 架构设计

```text
Wheel
 ├── 接收 Schedule / Reset / Cancel / Advance
 ├── 管理 timer 生命周期和命令队列
 ├── 推进多个 Level
 └── 将到期 callback 提交给 WorkerPool

Level
 ├── 管理本层 tick 和 currentIndex
 ├── 计算 timer 在本层的目标桶
 └── 在层级回环时触发 cascade

Slot
 ├── 保存 timer
 ├── 使旧 timer entry 失效
 └── 返回当前桶中的有效 timer
```

当前层级示例：

```text
Level 0: 1ms  × 64  = 64ms
Level 1: 64ms × 64  = 4096ms
Level 2: 4096ms × 64 = 262144ms
```

timer 会优先尝试放入低层；低层覆盖范围不足时，才会放入更高层。高层回环时，timer 会重新计算剩余时间并向下分发。

## 📊 Benchmark

Benchmark 程序位于 [`benchmark/benchmark.go`](benchmark/benchmark.go)，可以通过下面的命令运行：

```bash
GOCACHE=/tmp/gocache-hyperwheel go run ./benchmark \
  -timers=10000 \
  -base-tick=1ms \
  -max-delay=1s \
  -slots=64
```

Benchmark 覆盖以下几个方面：

- Schedule 命令提交和应用；
- Reset、Cancel；
- 普通分布式到期；
- 同一桶集中到期；
- 长延迟跨层 cascade；
- 每个 tick 的延迟分布；
- slice + generation 与 linked-list slot 对比。

### 容量压测结果

以下结果来自本地 WSL2 环境：

```text
OS:  Ubuntu 22.04.5 LTS on WSL2
Kernel: 6.18.33.2-microsoft-standard-WSL2
CPU: 16 logical cores
Go:  1.26.5 linux/amd64
BaseTick: 1ms 或 10ms
SlotsPerLevel: 64
Callback: 空回调，只统计时间轮调度成本
重复次数: 每组配置运行 20 次，表格使用算术平均值
```

下面是 20 次运行的均值，单位为 `ns/op`。每组包含 2,000 个 timer；`slice` 指
slice + generation，`list` 指 linked-list。

| 配置 | Slot | Schedule | Reset + Apply | 分布式 Advance | 同桶 Advance | 长 cascade | Tick p50 / p95 / p99 |
|---|---|---:|---:|---:|---:|---:|---:|
| 8 slots, 64ms | slice | 314.07 | 135.85 | 121.75 | 67.05 | 141.21 | 1,829.70 / 12,613.95 / 15,023.10 |
| 8 slots, 64ms | list | 219.10 | 154.60 | 116.23 | 64.59 | 146.32 | 1,744.95 / 15,057.00 / 16,174.35 |
| 16 slots, 1s | slice | 323.96 | 142.14 | 165.72 | 71.13 | 230.48 | 187.55 / 1,435.25 / 2,007.55 |
| 16 slots, 1s | list | 241.02 | 161.45 | 180.26 | 69.18 | 238.54 | 189.20 / 1,750.20 / 2,508.35 |
| 64 slots, 1s | slice | 351.66 | 139.64 | 125.18 | 65.18 | 146.39 | 178.30 / 296.60 / 5,150.15 |
| 64 slots, 1s | list | 236.36 | 164.61 | 126.07 | 62.33 | 150.03 | 177.20 / 291.40 / 6,248.70 |
| 256 slots, 1s | slice | 323.59 | 131.78 | 114.89 | 65.78 | 144.20 | 178.10 / 269.65 / 750.85 |
| 256 slots, 1s | list | 210.90 | 148.14 | 114.92 | 60.59 | 151.58 | 177.25 / 268.70 / 677.95 |
| 8 slots, 1m | slice | 328.59 | 149.90 | 442.52 | 63.39 | 715.98 | 41.10 / 70.65 / 140.15 |
| 8 slots, 1m | list | 223.10 | 168.50 | 442.59 | 60.25 | 737.04 | 41.90 / 72.30 / 135.00 |

其中 tick 延迟列的完整均值为 p50 / p95 / p99。长延迟配置的 p99
受单次 cascade 和系统调度影响较大，不能只看平均值。

在技能 CD 最常见的 `BaseTick=1ms`、`MaxDelay=1s` 配置下，建议优先使用
`SlotsPerLevel=64` 或 `256`。根据这 20 次测试，slice + generation 的
`Reset + Apply` 通常比 linked-list 更快，适合高频技能 CD；linked-list
在 stale entry 较多时更有优势。

容量压测使用相同的 WSL2 环境。在
`BaseTick=1ms`、`MaxDelay=1s`、`SlotsPerLevel=64`、slice + generation 配置下，
下面是每组 20 次运行的均值：

| Timer 数量 | 分布式 Advance | 同一桶集中 Advance | Reset + Apply |
|-----------:|---------------:|-------------------:|--------------:|
| 10,000 | 约 1.22 ms | 约 0.73 ms | 约 1.40 ms |
| 50,000 | 约 7.44 ms | 约 4.82 ms | 约 9.21 ms |
| 100,000 | 约 20.60 ms | 约 13.33 ms | 约 25.98 ms |

在 `BaseTick=10ms`、`MaxDelay=10s` 下，还做过一次更大规模的探索性测试：

| Timer 数量 | 分布式 Advance | 同一桶集中 Advance | Reset + Apply |
|-----------:|---------------:|-------------------:|--------------:|
| 250,000 | 约 91.72 ms | 约 57.59 ms | 约 99.97 ms |
| 500,000 | 约 226.26 ms | 约 143.88 ms | 约 210.21 ms |

20 次均值表和探索性测试中的数字都表示一次批量操作的总耗时，不表示每个服务器
tick 都会产生同样的负载。测试结果说明：

- 几千到几万级技能 CD，时间轮本身通常不会成为主要瓶颈；
- 10 万级 timer 仍可以完成批量调度，但不应让它们在同一个服务器 tick 内全部到期；
- 25 万到 50 万级 timer 可以存放和处理，但必须把到期任务分散到多个 tick，并结合真实 callback 成本评估；
- 如果大量 timer 在同一个 tick 集中到期，单 tick 延迟会随到期数量近似线性增长；
- callback、网络发送、玩家状态更新和数据库操作通常比时间轮内部调度更昂贵，生产环境应单独测量完整业务链路。

因此，对于技能 CD，推荐先从下面的配置开始，并使用真实技能释放数据进行压测：

```go
BaseTick:      1 * time.Millisecond // 或服务器逻辑 tick
SlotsPerLevel: 64
SlotType:      timerwheel.SlotTypeSlice
```

Benchmark 结果只用于比较当前实现和发现趋势，不应直接作为所有机器、所有 callback 和所有服务器架构的性能承诺。

## 📦 当前状态

### ✅ 已实现

已实现：

- 多层时间轮；
- 基于服务器 tick 的 `Advance`；
- `Schedule`、`Cancel`、`Reset`；
- 非整数 tick 延迟向上取整；
- command channel 提交模型；
- slice + generation slot；
- linked-list slot；
- 两种 slot 的行为一致性测试；
- 可选 WorkerPool；
- Wheel 生命周期关闭和基础并发测试；
- benchmark 程序和多配置容量压测。

### 🧭 计划中的工作

- 更完善的 command 背压策略；
- callback panic 与 WorkerPool 生命周期策略；
- cascade 高峰期的分批迁移与预算控制；
- 更贴近真实游戏业务的长时间压力测试。

## 📚 设计文档

更详细的设计说明请阅读 [doc.md](doc.md)。
