---
name: screen-gif
description: Display animated GIF on MHS-3.5 screen during AI thinking process via FIFO control.
homepage: https://github.com/sipeed/picoclaw
metadata: {"nanobot":{"emoji":"🦞","requires":{"system":["fifo","framebuffer"]}}}
---

# Screen GIF (思考动画显示)

在 AI 思考过程中，通过 MHS-3.5 屏幕播放 GIF 动画，提供视觉反馈。

## 快速开始

```bash
# 1. 启动 GIF 播放器（后台运行）
cd /home/pi/.picoclaw/workspace
nohup ./gif_player > gif_player.log 2>&1 &

# 2. 验证播放器运行
ps aux | grep gif_player

# 3. 手动测试控制
echo "1" > /tmp/picoclaw_animation  # 亮屏播放
echo "0" > /tmp/picoclaw_animation  # 黑屏停止
```

## 工作原理

### 系统架构

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  PicoClaw Agent │────▶│   FIFO 管道      │────▶│   GIF Player    │
│  (loop.go)      │     │ /tmp/picoclaw_   │     │  (gif_player.go)│
│                 │     │   animation      │     │                 │
│  思考开始 ──────┼────▶│   "1" = 播放     │────▶│  读取 GIF 帧    │
│  思考结束 ──────┼────▶│   "0" = 黑屏     │────▶│  输出到 fb1     │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                                                        │
                                                        ▼
                                               ┌─────────────────┐
                                               │  Framebuffer    │
                                               │  /dev/fb1       │
                                               │  MHS-3.5 屏幕   │
                                               └─────────────────┘
```

### 代码修改点

**1. PicoClaw Agent (`pkg/agent/loop.go`)**

```go
// 动画控制 FIFO 路径
const animationFifoPath = "/tmp/picoclaw_animation"

// 向 FIFO 写入动画控制信号
func writeAnimationSignal(active bool) {
    fd, err := unix.Open(animationFifoPath, unix.O_WRONLY|unix.O_NONBLOCK, 0666)
    if err != nil {
        return // FIFO 可能还没创建，忽略错误
    }
    defer unix.Close(fd)
    
    signal := "0"
    if active {
        signal = "1"
    }
    unix.Write(fd, []byte(signal+"\n"))
}

// 激活动画（亮屏）并重置关闭定时器
func activateAnimation() {
    timerMu.Lock()
    defer timerMu.Unlock()
    
    // 取消之前的关闭定时器
    if animationTimer != nil {
        animationTimer.Stop()
    }
    
    // 亮屏
    writeAnimationSignal(true)
    
    // 计划在 30 秒后自动关闭（如果期间没有新消息）
    animationTimer = time.AfterFunc(30*time.Second, func() {
        writeAnimationSignal(false)
    })
}

// 在 runAgentLoop() 中调用
func (al *AgentLoop) runAgentLoop(...) {
    // 激活动画（亮屏）- 每次处理消息时调用
    activateAnimation()
    
    // ... 处理消息 ...
    
    // 消息处理完成后关闭屏幕
    defer writeAnimationSignal(false)
}
```

**2. GIF 播放器 (`gif_player.go`)**

```go
const (
    fifoPath = "/tmp/picoclaw_animation"
)

// 从 FIFO 读取控制信号
func readFromFifo(fifoPath string) bool {
    file, err := os.Open(fifoPath)
    if err != nil {
        return false
    }
    defer file.Close()
    
    reader := bufio.NewReader(file)
    data, err := reader.ReadString('\n')
    if err != nil {
        return false
    }
    
    return data == "1" || data == "start"
}

// 主循环
for {
    select {
    case active := <-fifoChan:
        isBlackScreen = !active
        if active {
            frameIndex = 0  // 从头开始播放
        }
    default:
    }
    
    if isBlackScreen {
        fb.WriteAt(blackFrame, 0)  // 黑屏
    } else {
        fb.WriteAt(frameBuf, 0)    // 显示 GIF 帧
        frameIndex++
    }
}
```

## 配置选项

### 修改 GIF 文件

```bash
# 替换 GIF 文件（保持文件名不变）
cp your_animation.gif /home/pi/.picoclaw/workspace/dragon.gif

# 或者修改 gif_player.go 中的路径
# GIF_PATH := "your_custom.gif"
```

### 调整自动关闭时间

编辑 `pkg/agent/loop.go`:

```go
// 修改 30 秒为其他值
animationTimer = time.AfterFunc(60*time.Second, func() {  // 改为 60 秒
    writeAnimationSignal(false)
})
```

### 修改帧延迟

编辑 `gif_player.go`:

```go
// 统一帧延迟（降低刷新率）
delay := 80  // 每帧固定 80ms，可调整为 50-200ms
```

## 硬件要求

| 组件 | 要求 |
|------|------|
| 屏幕 | MHS-3.5 (ST7789, 480x320) |
| Framebuffer | `/dev/fb1` 可用 |
| 系统 | Linux (支持 FIFO 和 framebuffer) |
| 内存 | ≥64MB (GIF 预渲染需要) |

## 故障排除

| 问题 | 解决方案 |
|------|----------|
| GIF 播放器无法启动 | 检查 `/dev/fb1` 是否存在，确认屏幕驱动已加载 |
| 屏幕不亮 | 检查 framebuffer 权限，尝试 `chmod 666 /dev/fb1` |
| FIFO 不存在 | PicoClaw 会自动创建，或手动 `mknod /tmp/picoclaw_animation p` |
| 动画卡顿 | 降低 GIF 分辨率或帧数，增加帧延迟 |
| 内存不足 | 使用更小的 GIF 文件，或减少预渲染帧数 |
| 屏幕不自动关闭 | 检查 `activateAnimation()` 是否被正确调用 |

## 手动控制命令

```bash
# 启动播放器
cd /home/pi/.picoclaw/workspace && ./gif_player &

# 播放 GIF
echo "1" > /tmp/picoclaw_animation

# 停止播放（黑屏）
echo "0" > /tmp/picoclaw_animation

# 查看播放器状态
ps aux | grep gif_player
cat /tmp/picoclaw_animation  # 会阻塞，用于测试

# 重启播放器
./restart_gif_player.sh

# 查看日志
tail -f gif_player.log
```

## 相关文件

| 文件 | 描述 |
|------|------|
| `pkg/agent/loop.go` | PicoClaw 主循环，包含动画控制逻辑 |
| `gif_player.go` | Go 语言编写的 GIF 播放器 |
| `dragon.gif` | 默认播放的 GIF 文件 |
| `restart_gif_player.sh` | 重启播放器脚本 |
| `/tmp/picoclaw_animation` | FIFO 控制管道 |

## 省电模式

对话结束后会自动关闭屏幕以省电：

```go
// 在 runAgentLoop() 的 defer 中
defer func() {
    // 消息处理完成后关闭屏幕
    writeAnimationSignal(false)
}()
```

## 扩展功能

### 添加多个动画

```go
// 根据消息类型选择不同 GIF
if isThinking {
    playGif("thinking.gif")
} else {
    playGif("idle.gif")
}
```

### 添加亮度控制

```go
// 通过修改 framebuffer 实现
func setBrightness(level uint8) {
    // 调整 RGB565 颜色值
}
```

## 注意事项

⚠️ **重要提示**:
- GIF 播放器必须在 PicoClaw 启动前运行
- FIFO 路径必须一致 (`/tmp/picoclaw_animation`)
- 确保有足够的内存预渲染 GIF 帧
- 使用非阻塞模式写入 FIFO，避免阻塞主进程
- 屏幕分辨率必须匹配 (480x320)

## 许可证

MIT License - 与 PicoClaw 主项目保持一致
