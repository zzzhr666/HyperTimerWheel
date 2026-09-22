# HyperTimerWheel 设计文档

## 1. 目标

HyperTimerWheel 面向游戏服务器中大量短期和中长期定时任务的管理，例如：

- 技能冷却；
- buff、debuff 和状态过期；
- 定时邮件；
- 改名冷却；
- 延迟通知；
- 房间、匹配和战斗状态超时；
- 定时清理任务。

项目当前的主要目标是：

1. 使用多层时间轮覆盖不同时间范围；
2. 让时间推进适配游戏服务器自己的 tick；
3. 将业务 goroutine 的提交与时间轮内部修改解耦；
4. 让桶的底层实现可以替换；
5. 为后续性能优化和生产化完善保留空间。

## 2. 整体结构

```text
业务 goroutine
    │
    ├── Schedule
    ├── Reset
    └── Cancel
          │
          ▼
      commands channel
          │
          ▼
服务器 tick goroutine
    └── Advance(now)
          │
          ├── drainCommands
          ├── 推进 Level 0
          ├── 必要时递归推进上层 Level
          ├── cascade
          └── 执行最低层当前桶
                    │
                    ├── WorkerPool
                    └── callback
```

代码中主要有五类对象：

| 对象           | 主要职责                              |
|--------------|-----------------------------------|
| `Wheel`      | 管理整体时间、命令、timer 生命周期和层级推进         |
| `level`      | 管理单层 tick、桶索引和本层放置算法              |
| `slot`       | 管理单个桶中的 timer                     |
| `timer`      | 保存 timer ID、callback、deadline 和状态 |
| `WorkerPool` | 执行到期 callback                     |

## 3. 多层时间轮

配置由三个核心参数决定：

```text
type WheelConfig struct {
BaseTick      time.Duration
MaxDelay      time.Duration
SlotsPerLevel int
SlotType      SlotType
StartTime     time.Time
}
```

假设：

```text
BaseTick      = 1ms
SlotsPerLevel = 8
```

各层的 tick 和覆盖范围为：

```text
Level 0:
    tick = 1ms
    span = 1ms × 8 = 8ms

Level 1:
    tick = 8ms
    span = 8ms × 8 = 64ms

Level 2:
    tick = 64ms
    span = 64ms × 8 = 512ms
```

创建时间轮时，项目会不断扩大层级，直到所有合法的 `MaxDelay` 都能被覆盖。

### 3.1 Timer 放置规则

添加 timer 时，Wheel 会从最低层开始尝试：

```text
Level 0 能覆盖
    └── 放入 Level 0

Level 0 不能覆盖
    └── 尝试 Level 1

Level 1 不能覆盖
    └── 尝试 Level 2
```

每个 Level 自己负责计算目标桶，Wheel 不直接访问 Level 的内部桶数组。

最低层需要保证不提前触发：

```text
BaseTick = 1ms
Delay    = 1.5ms
目标 tick = ceil(1.5 / 1) = 2
```

因此这个 timer 会在 2ms tick 处触发。

高层的职责是覆盖时间范围，不承担最终精确触发。高层 timer 在接近到期时间时，会逐层向下重新分发。

## 4. Advance 和层级推进

`Advance` 是时间轮的核心推进入口：

```text
func (w *Wheel) Advance(now time.Time) int
```

它不会直接跳过中间层级，而是根据经过的 base tick 逐格推进：

```text
elapsed := int(now.Sub(w.lastTime) / w.baseTick)
```

每推进一个 base tick，执行以下流程：

```text
1. lastTime 前进一个 baseTick
2. Level 0 前进一格
3. 如果 Level 0 回环，递归推进 Level 1
4. 如果上层发生推进，对应 Level 执行 cascade
5. 取出 Level 0 当前桶
6. 处理取消、触发或提交 callback
```

层级回环关系如下：

```text
Level 0 每个 base tick 推进
    │
    └── Level 0 回到 0 时推进 Level 1
            │
            └── Level 1 回到 0 时推进 Level 2
```

## 5. Cascade 分发

当高层 Level 推进到新的当前桶时，Wheel 会取出这个桶中的 timer，并重新计算：

```text
remaining := timer.deadline.Sub(w.lastTime)
```

然后根据剩余时间再次选择合适的 Level。

```text
高层当前桶
    │
    ├── timer 已到期
    │      └── 放入 Level 0 当前桶
    │
    └── timer 未到期
           └── 从低层开始重新选择目标 Level
```

这样设计的原因是：timer 初次放入高层时，只需要粗粒度地表示时间范围；随着时间推进，它必须逐渐下沉到低层，最终由 Level 0 负责触发。

Cascade 可能让某一个 tick 集中处理大量 timer，因此后续可以考虑：

- 限制每次 cascade 的处理预算；
- 分批迁移；
- 将迁移工作拆成多个服务器 tick；
- 统计每层 cascade 数量；
- 对高峰场景做 benchmark。

当前版本优先保持逻辑简单和语义清晰，尚未做分批迁移。

## 6. Slot 抽象

当前的 Slot 接口是：

```text
type slot interface {
add(t *timer)
invalidate(t *timer) bool
takeAll() []*timer
}
```

Wheel 和 Level 只依赖这些操作，不关心桶的具体存储方式。

当前支持的配置值：

```text
SlotTypeSlice
```

当前支持 `SlotTypeSlice` 和 `SlotTypeLinkedList` 两种实现。两者遵循相同的
Slot 语义，Wheel 和 Level 不依赖具体的底层存储方式。

## 7. Slice + Generation Slot

当前 slice slot 使用以下结构：

```text
type sliceEntry struct {
timer *timer
gen   uint64
}

type sliceSlot struct {
entries     []sliceEntry
generations map[*timer]uint64
}
```

### 7.1 添加

第一次添加 timer：

```text
generation[timer] = 0
entry.gen         = 0
```

### 7.2 Reset 或失效

slot 不会在线性数组中查找并删除 timer，而是增加 generation：

```text
旧 entry.gen         = 0
current generation   = 1
```

旧 entry 因为 generation 不一致而失效。

如果 timer 重新添加：

```text
旧 entry: gen = 0，失效
新 entry: gen = 1，有效
```

### 7.3 取出

`takeAll` 会遍历当前桶：

```text
if entry.gen != s.generations[entry.timer] {
continue
}
```

取出完成后，slot 会清空 entries 和 generation map。

### 7.4 优缺点

优点：

- 失效操作接近 O(1)；
- 添加操作简单；
- timer 不需要携带底层节点；
- 连续内存更利于 CPU cache；
- 不需要在切片中搬移元素。

代价：

- Reset 越频繁，旧 entry 越多；
- `takeAll` 需要扫描失效 entry；
- 在桶被取出前可能暂时占用更多内存；
- generation map 有额外的哈希表开销。

linked-list slot 使用通用双向链表和 timer 到 iterator 的索引，Reset/Invalidate
可以直接摘除旧节点；它减少 stale entry 扫描，但会带来节点和 map 的额外开销。
两种实现已经有一致性测试，并通过 benchmark 进行对比。

## 8. Timer 生命周期

Timer 状态为：

```text
timerActive
timerCanceled
timerFired
```

状态转换：

```text
             ┌──────────────┐
             │              ▼
        active ─────────> canceled
             │
             ▼
           fired
```

`Cancel` 和触发都使用原子 CAS，因此同一个 timer 最多只有一个终态转换成功。

Timer 被取消时不会立即从 slot 中物理删除，而是采用惰性清理：

```text
Cancel
  └── 修改 timer.state

后续 takeAll
  └── 发现 timer 非 active，跳过并清理索引
```

Reset 则会：

```text
1. 让旧 slot 中的 entry 失效
2. 更新 deadline
3. 根据新 delay 重新放入时间轮
```

## 9. 并发模型

当前实现采用“多生产者提交、单消费者推进”的模型：

```text
Schedule / Reset
    └── 写入 commands channel

Cancel
    └── 原子修改 timer.state

Advance
    └── 唯一修改 levels、maps 和 slots 的线程
```

推荐的游戏服务器使用方式：

```text
业务 goroutine:
    Schedule / Reset / Cancel

服务器主循环:
    Advance(now)
```

当前不建议多个 goroutine 同时调用 `Advance`，因为时间轮内部结构不是多写者并发结构。

当前的生命周期约定是：`Schedule`、`Reset` 和 `Cancel` 可以由业务 goroutine 并发调用，但调用 `Close` 前，业务层必须先停止新的
`Schedule`/`Reset` 提交，并等待已经运行的提交 goroutine 完成。`Close` 与提交操作同时发生不属于当前保证的使用场景。

推荐的关闭顺序：

```text
停止业务生产者
    │
    ▼
等待生产者 goroutine 退出
    │
    ▼
调用 Wheel.Close
```

### 9.1 Command channel

Schedule 和 Reset 不直接改动 Wheel 内部，而是提交 command：

```text
type command struct {
kind  commandKind
t     *timer
delay time.Duration
}
```

下一次 `Advance` 时调用 `drainCommands`，按顺序应用命令。

这样可以把复杂的 map、level、slot 修改集中到单一推进线程中，避免在核心路径加锁。

当前 command channel 是有容量的 channel。容量耗尽时，当前实现会立即返回失败，不会阻塞提交方。后续可以根据服务器需求选择：

- 返回队列满错误；
- 扩大容量；
- 批量提交；
- 增加外部 MPSC 队列；
- 设计明确的背压策略。

当前实现使用非阻塞发送：队列满时，`Schedule` 返回 `ErrCommandQueueFull`，`Reset` 返回 `false`。`CommandCapacity` 未配置或小于等于
0 时，会回退到 `DefaultCommandsCapacity`。

## 10. Callback 和 WorkerPool

时间轮负责判断 timer 到期，不负责业务逻辑本身。

触发时会创建 callback task：

```text
task := func () {
t.cb(now)
}
```

如果配置了 WorkerPool，则优先提交到 WorkerPool；WorkerPool 队列满时，当前实现会回退到 `Advance` 所在线程执行。

因此：

```text
Advance 返回值 = 从 active 转为 fired 的 timer 数量
```

不等于 callback 已经执行完成的数量。

如果业务 callback 可能阻塞或执行时间较长，应该配置 WorkerPool，并避免在 callback 中反向阻塞时间轮推进线程。

## 11. Wheel 生命周期

Wheel 有两个状态：

```text
WheelStateRunning
WheelStateStopped
```

调用 `Close` 后，状态从 Running 转为 Stopped：

```text
Close
 ├── 拒绝新的 Schedule
 ├── 拒绝 Reset 和 Cancel
 ├── 让 Advance 变成 no-op
 └── 等待 WorkerPool 中已有任务完成
```

`Close` 使用 CAS，因此可以安全地重复调用：

```text
w.Close()
w.Close()
```

当前设计不主动关闭 `commands` channel，避免并发提交者向已关闭 channel 写入导致 panic。使用方应在关闭前停止新的业务提交。

## 12. 当前限制

当前实现还存在以下限制：

1. `Advance` 需要由单个 goroutine 调用；
2. command channel 满时，Schedule/Reset 会立即返回失败；
3. callback panic 处理策略尚未定义；
4. WorkerPool 的配置和 Wheel 生命周期仍可继续完善；
5. cascade 尚未做分批迁移和预算控制；
6. benchmark 当前主要使用空 callback，尚未覆盖完整业务链路。

## 13. Benchmark

Benchmark 位于 `benchmark/benchmark.go`，当前包含 Schedule、Reset、Cancel、
普通 Advance、同桶集中到期、长延迟 cascade 和 tick 延迟分布等场景，并同时
比较 slice + generation 与 linked-list 两种 Slot。

测试环境：

```text
Ubuntu 22.04.5 LTS on WSL2
Linux 6.18.33.2-microsoft-standard-WSL2
Go 1.26.5 linux/amd64
16 logical cores
```

在 2,000 个 timer、`BaseTick=1ms`、`MaxDelay=1s`、`SlotsPerLevel=64`
的技能 CD 近似配置下，每组独立运行 20 次。slice + generation 的均值为：

```text
Schedule + Apply:       351.66 ns/op
Reset + Apply:           139.64 ns/op
分布式 Advance:          125.18 ns/op
同一桶集中 Advance:       65.18 ns/op
长延迟 cascade:          146.39 ns/op
tick p50 / p95 / p99:   178.30 / 296.60 / 5150.15 ns
```

容量阶梯测试中，1ms tick、1s 最大延迟、64 slots 配置下，slice + generation
每组运行 20 次的均值如下：

```text
10,000 timers:   distributed 1.22ms, same-bucket 0.73ms, reset 1.40ms
50,000 timers:   distributed 7.44ms, same-bucket 4.82ms, reset 9.21ms
100,000 timers:  distributed 20.60ms, same-bucket 13.33ms, reset 25.98ms
```

这些测试使用空 callback，只衡量时间轮调度成本。实际游戏服务器还需要把
callback、网络发送、玩家状态更新、锁竞争和 GC 纳入完整压测。时间轮容量的
关键指标是单个 tick 内到期和 cascade 的 timer 数量，而不是仅仅看总 timer 数。

## 14. 后续路线

建议按照以下顺序继续：

```text
第一阶段：框架完整性
    ├── 完善 Wheel.Close 测试
    ├── 明确 Advance 的单写者契约
    ├── 明确 command channel 背压策略
    └── 补充错误和生命周期文档

第二阶段：底层实现
    ├── 维护 linked-list slot
    ├── 维护两个 slot 实现的行为一致性测试
    └── 持续补充 Add / Reset / TakeAll benchmark

第三阶段：性能优化
    ├── 已完成基础 cascade 和容量 benchmark
    ├── 评估分批 cascade
    ├── 优化 map 和内存复用
    └── 分析 callback 提交和 WorkerPool 背压

第四阶段：生产化
    ├── 定义 Close 与并发提交的严格语义
    ├── 增加 callback panic 处理
    ├── 增加 metrics
    └── 增加压力测试和长时间稳定性测试
```
