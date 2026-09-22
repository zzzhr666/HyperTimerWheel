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
- 当前桶实现使用 slice + generation，后续可扩展链表实现。

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

## 📦 当前状态

### ✅ 已实现

已实现：

- 多层时间轮；
- 基于服务器 tick 的 `Advance`；
- `Schedule`、`Cancel`、`Reset`；
- 非整数 tick 延迟向上取整；
- command channel 提交模型；
- slice + generation slot；
- 可选 WorkerPool；
- Wheel 生命周期关闭和基础并发测试。

### 🧭 计划中的工作

- 链表 slot 实现；
- slice slot 与链表 slot benchmark；
- 更完善的 command 背压策略；
- callback panic 与 WorkerPool 生命周期策略；
- cascade 高峰期的进一步优化。

## 📚 设计文档

更详细的设计说明请阅读 [doc.md](doc.md)。
