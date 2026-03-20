# Screen GIF Skill - 思考动画显示

🦞 在 AI 思考过程中，通过 MHS-3.5 屏幕播放 GIF 动画，提供视觉反馈。

## 功能特点

- ✨ **实时反馈**: AI 开始思考时自动播放动画
- ⏱️ **自动关闭**: 对话结束后 30 秒自动关闭屏幕（省电）
- 🎬 **流畅播放**: GIF 帧预渲染，播放流畅不卡顿
- 📡 **FIFO 控制**: 通过命名管道进行进程间通信
- 🔧 **易于定制**: 可更换 GIF 文件、调整延迟时间

## 快速安装

```bash
# 1. 进入技能目录
cd /home/pi/.picoclaw/workspace/skills/screen-gif

# 2. 运行安装脚本
./install.sh

# 3. 完成！PicoClaw 会自动控制屏幕
```

## 手动安装

### 1. 编译 GIF 播放器

```bash
cd /home/pi/.picoclaw/workspace
go build -o gif_player gif_player.go
```

### 2. 启动播放器

```bash
# 后台运行
nohup ./gif_player > gif_player.log 2>&1 &

# 或使用 systemd
sudo systemctl start gif_player
sudo systemctl enable gif_player  # 开机自启
```

### 3. 测试控制

```bash
echo "1" > /tmp/picoclaw_animation  # 播放 GIF
echo "0" > /tmp/picoclaw_animation  # 黑屏停止
```

## 系统架构

```
PicoClaw Agent (loop.go)
         │
         │ 写入 FIFO
         ▼
/tmp/picoclaw_animation (命名管道)
         │
         │ 读取控制信号
         ▼
GIF Player (gif_player.go)
         │
         │ 渲染帧
         ▼
/dev/fb1 (Framebuffer)
         │
         ▼
MHS-3.5 屏幕 (480x320)
```

## 控制信号

| 信号 | 效果 |
|------|------|
| `1` 或 `start` | 开始播放 GIF |
| `0` 或 `stop` | 黑屏停止 |

## 自定义配置

### 更换 GIF 文件

```bash
# 替换默认 GIF（保持文件名不变）
cp your_animation.gif /home/pi/.picoclaw/workspace/dragon.gif

# 重启播放器
sudo systemctl restart gif_player
```

### 调整自动关闭时间

编辑 `/home/pi/picoclaw/pkg/agent/loop.go`:

```go
// 修改 30 秒为其他值（单位：秒）
animationTimer = time.AfterFunc(60*time.Second, func() {
    writeAnimationSignal(false)
})
```

重新编译 PicoClaw:

```bash
cd /home/pi/picoclaw
go build -o picoclaw ./cmd/picoclaw
```

### 调整帧率

编辑 `gif_player.go`:

```go
// 修改帧延迟（单位：毫秒）
delay := 80  // 降低数值提高帧率，增加数值降低帧率
```

重新编译播放器:

```bash
cd /home/pi/.picoclaw/workspace
go build -o gif_player gif_player.go
sudo systemctl restart gif_player
```

## 故障排除

### 播放器无法启动

```bash
# 检查 framebuffer 设备
ls -l /dev/fb1

# 检查权限
sudo chmod 666 /dev/fb1

# 查看日志
tail -f /home/pi/.picoclaw/workspace/gif_player.log
```

### 屏幕不亮

```bash
# 手动测试
echo "1" > /tmp/picoclaw_animation

# 检查播放器进程
ps aux | grep gif_player

# 重启播放器
sudo systemctl restart gif_player
```

### 动画卡顿

- 使用更小尺寸的 GIF（建议 ≤480x320）
- 减少 GIF 帧数
- 增加帧延迟时间

### 内存不足

```bash
# 查看内存使用
free -h

# 使用更小的 GIF 文件
# 或减少预渲染帧数（需修改 gif_player.go）
```

## 相关文件

| 文件 | 描述 |
|------|------|
| `SKILL.md` | 技能文档 |
| `install.sh` | 安装脚本 |
| `gif_player.go` | GIF 播放器源码 |
| `dragon.gif` | 默认 GIF 文件 |
| `gif_player.service` | systemd 服务配置 |

## 技术细节

### FIFO 通信

```go
// PicoClaw 写入
fd, _ := unix.Open("/tmp/picoclaw_animation", unix.O_WRONLY|unix.O_NONBLOCK, 0666)
unix.Write(fd, []byte("1\n"))  // 或 "0\n"

// GIF Player 读取
file, _ := os.Open("/tmp/picoclaw_animation")
reader := bufio.NewReader(file)
data, _ := reader.ReadString('\n')
```

### Framebuffer 输出

```go
// 打开 framebuffer
fb, _ := os.OpenFile("/dev/fb1", os.O_WRONLY, 0)

// 写入帧数据（RGB565 格式）
fb.WriteAt(frameBuf, 0)
```

### RGB565 颜色转换

```go
func rgb565(c color.Color) uint16 {
    r, g, b, _ := c.RGBA()
    r5 := uint16(r >> 11)
    g6 := uint16(g >> 10)
    b5 := uint16(b >> 11)
    return (r5 << 11) | (g6 << 5) | b5
}
```

## 许可证

MIT License - 与 PicoClaw 主项目保持一致

## 贡献

欢迎提交 Issue 和 Pull Request！

- 项目地址：https://github.com/sipeed/picoclaw
- 问题反馈：https://github.com/sipeed/picoclaw/issues

---

🦞 *"Every bit helps, every bit matters."*
