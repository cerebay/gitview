# gv — git view

[English](README.md) | **简体中文**

一个轻量、好看的终端 Git 面板，类似 VS Code 的「源代码管理」视图。

![gv 截图](docs/screenshot.png)

- 分支卡片：当前分支、上游分支、领先/落后、未发布/已同步状态、工作区更改数
- 提交列表：默认显示最近 6 条，往下滚动查看更多（最多 200 条）；`feat:`、`fix:`、`scope:` 这类前缀会按类型着色
- 分支列表：切换分支、删除分支（未合并的分支会再确认是否强制删除）
- 一键发布 / 推送
- 每 3 秒自动刷新；支持键盘和鼠标
- 自带深色背景（`rgb(30, 31, 41)`），在白色背景的终端里也能正常显示

## 安装

需要 Go 1.24+ 和 `git`。

```bash
go install github.com/cerebay/gv@latest
# 或者
git clone https://github.com/cerebay/gv && cd gv && go build -o ~/.local/bin/gv .
```

## 使用

```bash
gv             # 当前仓库
gv ~/code/xx   # 指定仓库
```

标准输出不是终端时（例如通过管道），只输出一段纯文本摘要。

| 按键 | 功能 |
|---|---|
| `j` / `k`、`↑` / `↓` | 移动 |
| `Tab` | 在「提交」和「分支」两栏之间跳转（`c` / `b` 直接跳到对应栏） |
| `g` / `G` | 跳到顶部 / 底部 |
| `Enter` | 提交：查看详情 · 分支：切换 · 标题：折叠/展开 |
| `d` | 删除选中的分支 |
| `p` | 发布（`push -u origin <分支>`）或推送 |
| `r` | 刷新 |
| `q` | 退出 |

鼠标：点击选中，再点一次打开，点击标题折叠，滚轮滚动。

## 环境变量

| 变量 | 作用 |
|---|---|
| `GV_BG=none` | 不绘制深色背景，使用终端自己的背景 |
| `GV_COLOR=truecolor` / `256` | 强制指定色彩模式。默认情况下，只要 `TERM` 带 `256color` 就按真彩色输出，即使没有设置 `COLORTERM`（macOS 自带的 Terminal.app 除外） |

> 注意：很多 shell（比如 oh-my-zsh 的 git 插件）里 `gp` 是 `git push` 的别名，所以这个工具叫 `gv`。

基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 和 [Lip Gloss](https://github.com/charmbracelet/lipgloss) 构建。

## 许可证

MIT
