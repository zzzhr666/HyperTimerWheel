# 🚀 HyperTimerWheel

一个面向游戏服务器的 Go 多层时间轮。

HyperTimerWheel 适合管理技能 CD、Buff/Debuff 过期、定时邮件、改名冷却、延迟通知、房间超时和其他需要延迟执行的任务。

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)

## ✨ 项目简介

HyperTimerWheel 使用多层时间轮覆盖不同的时间范围：

- 低层时间轮提供较细的触发粒度；
- 高层时间轮覆盖更长的延迟；
- 由游戏服务器自己的 tick 调用 `Advance`；
- 业务 goroutine 通过 command channel 提交 `Schedule` 和 `Reset`；
- `Cancel` 使用 timer 状态原子操作；
- callback 可以直接在推进线程执行，也可以交给 WorkerPool；
- callback panic 会被恢复并上报，不会破坏时间轮推进或 WorkerPool worker；
- 单个桶支持 slice + generation 和 linked-list 两种实现。

## 🧱 架构设计

```text
业务 goroutine
    │
    ├── Schedule / Reset / Cancel
    │
    ▼
commands channel
    │
    ▼
服务器 tick goroutine
    │
    └── Advance(now)
          ├── 应用提交命令
          ├── 推进 Level
          ├── 递归触发上层 cascade
          ├── 取出最低层到期 timer
          └── 执行或提交 callback
```

主要组件：

- `Wheel`：管理时间、timer 生命周期、命令队列和多个 `Level`；
- `Level`：管理本层 tick、当前桶索引和 timer 的目标桶；
- `Slot`：抽象单个桶的存储方式；
- `Timer`：保存 ID、callback、deadline 和状态；
- `WorkerPool`：异步执行到期 callback。

例如 `BaseTick=1ms`、`SlotsPerLevel=64` 时：

```text
Level 0: 1ms    × 64 = 64ms
Level 1: 64ms   × 64 = 4096ms
Level 2: 4096ms × 64 = 262144ms
```

Timer 会优先放入能够覆盖它的最低层。高层回环时，timer 会根据剩余时间逐层向下分发，最终由最低层负责触发。

时间轮采用多生产者、单推进者模型：

- `Schedule`、`Reset` 可以由多个业务 goroutine 调用；
- `Cancel` 可以由多个业务 goroutine 调用；
- `Advance` 应由一个固定的服务器 tick goroutine 调用；
- Level、Slot 和 timer map 由推进线程统一修改。

## 🏁 使用方法

```go
package main

import (
	"fmt"
	"time"

	timerwheel "HyperTimerWheel"
)

func main() {
	start := time.Now()

	wheel, err := timerwheel.NewWheel(timerwheel.WheelConfig{
		BaseTick:      10 * time.Millisecond,
		MaxDelay:      time.Minute,
		SlotsPerLevel: 64,
		SlotType:      timerwheel.SlotTypeSlice,
		StartTime:     start,
		WorkerPoolConfig: timerwheel.WorkerPoolConfig{
			WorkerNum: 4,
			QueueSize: 1024,
		},
		CallbackPanicHandler: func(info timerwheel.CallbackPanicInfo) {
			fmt.Printf("timer %d panicked: %v\n", info.ID, info.PanicValue)
		},
	})
	if err != nil {
		panic(err)
	}
	defer wheel.Close()

	_, err = wheel.Schedule(500*time.Millisecond, func(now time.Time) {
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
}
```

核心操作：

```go
handle, err := wheel.Schedule(delay, callback)
ok := wheel.Reset(handle, newDelay)
ok = wheel.Cancel(handle)
fired := wheel.Advance(now)
wheel.Close()
```

延迟必须大于 `0` 且小于 `MaxDelay`。不是基础 tick 整数倍的延迟会向上取整，保证 timer 不会提前触发。

command channel 是有容量且非阻塞的。队列满时，`Schedule` 返回 `ErrCommandQueueFull`，`Reset` 返回 `false`。调用 `Close` 前，应停止新的 `Schedule`/`Reset` 提交并等待提交 goroutine 退出。

callback 可以直接执行，也可以交给 WorkerPool。WorkerPool 队列满时会回退到 `Advance` 线程执行。所有 callback 执行路径都会恢复 panic，并通过 `CallbackPanicHandler` 上报 timer ID、逻辑执行时间、panic 值和 stack；未配置 handler 时使用标准日志输出。

## 📊 Benchmark

Benchmark 程序位于 [`benchmark/benchmark.go`](benchmark/benchmark.go)：

```bash
GOCACHE=/tmp/gocache-hyperwheel go run ./benchmark \
  -timers=10000 \
  -base-tick=1ms \
  -max-delay=1s \
  -slots=64
```

测试覆盖 Schedule、Reset、Cancel、分布式到期、同桶集中到期、长延迟 cascade、tick 延迟分布，以及 slice + generation 和 linked-list 两种 Slot 的对比。

测试环境为 Ubuntu 22.04.5 LTS on WSL2、Linux amd64、Go 1.26.5、16 logical cores。技能 CD 近似配置为 `BaseTick=1ms`、`MaxDelay=1s`、`SlotsPerLevel=64`，每组运行 20 次，以下为 slice + generation 的平均结果：

```text
2,000 timers:
  Schedule + Apply:       351.66 ns/op
  Reset + Apply:           139.64 ns/op
  分布式 Advance:          125.18 ns/op
  同桶集中 Advance:         65.18 ns/op
  长延迟 cascade:          146.39 ns/op
  tick p50 / p95 / p99:   178.30 / 296.60 / 5150.15 ns
```

容量阶梯测试同样采用 20 次平均：

| Timer 数量 | 分布式 Advance | 同桶集中 Advance | Reset + Apply |
|---:|---:|---:|---:|
| 10,000 | 约 1.22ms | 约 0.73ms | 约 1.40ms |
| 50,000 | 约 7.44ms | 约 4.82ms | 约 9.21ms |
| 100,000 | 约 20.60ms | 约 13.33ms | 约 25.98ms |

Benchmark 使用空 callback，只衡量时间轮调度成本。实际项目还应把 callback、网络发送、玩家状态更新、锁竞争和 GC 纳入完整业务压测。时间轮的关键负载指标是单个 tick 内到期和 cascade 的 timer 数量，而不只是 timer 总数。

## 📚 设计文档

更详细的设计目标、数据结构、层级推进、Slot 实现、并发模型和生命周期说明见 [doc.md](doc.md)。
