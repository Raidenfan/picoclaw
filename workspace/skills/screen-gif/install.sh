#!/bin/bash
# 🦞 PicoClaw GIF 动画显示系统安装脚本
# 自动配置 MHS-3.5 屏幕的 GIF 播放功能

set -e

echo "========================================"
echo "  🦞 PicoClaw GIF 动画显示系统安装"
echo "========================================"
echo ""

WORKSPACE="/home/pi/.picoclaw/workspace"
SYSTEMD_DIR="/etc/systemd/system"

# 检查 GIF 播放器是否已编译
if [ ! -f "$WORKSPACE/gif_player" ]; then
    echo "📦 编译 GIF 播放器..."
    cd "$WORKSPACE"
    if [ -f "gif_player.go" ]; then
        go build -o gif_player gif_player.go
        echo "✅ GIF 播放器编译完成"
    else
        echo "❌ 错误：gif_player.go 不存在"
        exit 1
    fi
else
    echo "✅ GIF 播放器已存在"
fi

# 检查 FIFO 路径
FIFO_PATH="/tmp/picoclaw_animation"
echo "📝 FIFO 路径：$FIFO_PATH"

# 创建 systemd 服务文件
echo ""
echo "🔧 创建 systemd 服务..."

cat > "$WORKSPACE/gif_player.service" << EOF
[Unit]
Description=PicoClaw GIF Player for MHS-3.5 Screen
After=multi-user.target

[Service]
Type=simple
ExecStart=$WORKSPACE/gif_player
WorkingDirectory=$WORKSPACE
Restart=always
RestartSec=5
User=pi
StandardOutput=append:$WORKSPACE/gif_player.log
StandardError=append:$WORKSPACE/gif_player.log

[Install]
WantedBy=multi-user.target
EOF

echo "✅ 服务文件已创建：$WORKSPACE/gif_player.service"

# 询问是否启用自动启动
echo ""
read -p "是否启用开机自动启动？(y/n): " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    # 复制服务文件到 systemd 目录
    sudo cp "$WORKSPACE/gif_player.service" "$SYSTEMD_DIR/"
    sudo systemctl daemon-reload
    sudo systemctl enable gif_player.service
    sudo systemctl start gif_player.service
    
    echo ""
    echo "✅ 服务已启用并启动"
    echo ""
    echo "服务状态:"
    sudo systemctl status gif_player.service --no-pager
else
    echo ""
    echo "💡 手动启动命令:"
    echo "   sudo systemctl start gif_player"
    echo "   sudo systemctl enable gif_player  # 开机自启"
fi

# 测试 GIF 播放器
echo ""
echo "🧪 测试播放器..."
sleep 2
if pgrep -f "gif_player" > /dev/null; then
    echo "✅ GIF 播放器正在运行"
    echo ""
    echo "🎮 手动控制测试:"
    echo "   echo '1' > /tmp/picoclaw_animation  # 播放"
    echo "   echo '0' > /tmp/picoclaw_animation  # 停止"
else
    echo "⚠️  播放器未运行，请手动启动"
fi

echo ""
echo "========================================"
echo "  ✅ 安装完成！"
echo "========================================"
echo ""
echo "📚 使用说明:"
echo "  1. PicoClaw 会在思考时自动控制屏幕"
echo "  2. 对话结束后 30 秒自动关闭屏幕"
echo "  3. 手动控制：echo '1/0' > /tmp/picoclaw_animation"
echo ""
echo "📝 相关文件:"
echo "  - 播放器：$WORKSPACE/gif_player"
echo "  - 日志：$WORKSPACE/gif_player.log"
echo "  - 服务：$SYSTEMD_DIR/gif_player.service"
echo "  - GIF 文件：$WORKSPACE/dragon.gif"
echo ""
echo "🦞 Have fun!"
