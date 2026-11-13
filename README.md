# Tossy

Tossy 是一个磁盘清理分析器：用可下钻的旭日图展示磁盘被什么占满，给每一项标出安全等级，并提供"只动安全内容"的一键自动清理。它包含一个原生桌面应用（基于 [MyGo](https://github.com/egoist/mygo)）和一个命令行工具，两者共用同一套 Go 引擎。

支持 macOS、Linux 和 Windows。

## 功能

### 扫描与可视化

- **高速并行扫描**：多线程遍历目录树，生成紧凑、无指针的树结构，可承载数百万个条目。macOS 上用 `getattrlistbulk(2)` 批量读取目录项，Linux 用 `fstatat`，Windows 用 `FindFirstFile` 直接拿到大小与时间；自动跳过伪文件系统，默认不跨卷（可在设置中开启）。
- **旭日图**：按目录结构逐层展示占用，点击扇区下钻、面包屑或 Backspace 返回上级；删除或恢复时扇区会平滑变形。扫描过程中显示雷达扫描动画和实时计数。
- **列表与详情**：右侧面板显示当前文件夹的子项（按大小排序），选中文件后滑出详情，展示路径、大小、安全判定和原因。
- **磁盘用量**：顶部显示所在卷的总容量与剩余空间。

### 安全判定

引擎内置约 90 条规则，按名称和路径为每个节点给出四个等级之一，子项没有单独规则时继承父级判定：

| 等级 | 含义 | 示例 |
| --- | --- | --- |
| 安全 | 可重建或可重新下载 | 应用缓存、`node_modules`、`__pycache__`、npm / pnpm / Cargo / Go / Maven / Gradle 缓存、Xcode DerivedData、Homebrew 缓存 |
| 需确认 | 大概率可删，但应由用户确认 | Docker / 虚拟机磁盘、iOS 模拟器、本地大模型（Ollama、LM Studio、Hugging Face）、Python 虚拟环境、构建输出、下载目录、AI 编程助手会话记录 |
| 有风险 | 个人数据 | 文档、桌面、照片、邮件、`.ssh`、`.gnupg`、`.git` |
| 受保护 | 操作系统需要，Tossy 绝不删除 | `/System`、`/usr`、`C:\Windows`、`WinSxS` 等 |

部分规则会检查标记文件（例如 `Podfile`、`package.json`、`go.mod`），只在真正的项目目录里匹配 `dist`、`build`、`vendor` 之类的通用名称。

### 开发者目录

- **Git 仓库保护**：删除 Git 仓库或工作树前，会询问 git 是否有未提交的修改、未跟踪的文件，或只存在于本地、尚未推送的提交，并说明删除会丢失什么。
- **工作树一览**：仓库会列出共享同一对象库的所有工作树（AI 编程助手常会留下很多）。
- **项目构建缓存**：项目文件夹会列出其中的依赖与构建产物。

### 清理

- **一键自动清理**：扫描全盘的规则匹配项和已学会的清理习惯，先播放分析动画并显示评估清单（两者都可在设置中关闭），再把计划内的条目扔进废纸篓。被选中的只有可按需重建的内容和与你以往手动删除相同的内容。
- **使用中保护**：最近几分钟内有写入的条目（默认 10 分钟）和正在运行的应用的缓存会被跳过，避免把缓存从运行中的程序底下抽走。
- **手动删除**：可通过按钮、右键菜单、键盘（Delete / ⌘⌫）或拖到废纸篓桶删除单项。受保护的条目会抖动并说明原因，个人数据先询问，其余立即删除并可撤销。
- **清理习惯学习**：Tossy 会从手动删除中学习。每次删除被归纳为"父文件夹 + 形态"（子文件夹、扩展名或"超过两个月"），同一签名累计到一定分数后成为习惯，下次自动清理会一并带走并说明理由；恢复由习惯选中的条目会加倍扣分。习惯可在设置中查看、遗忘或关闭。

### 废纸篓

- **只记账、不搬动**：放入 Tossy 废纸篓只是在日志中记下标记，文件留在原处，也不会动系统废纸篓。恢复就是撤掉标记，"清空"才会在原位永久删除被标记的路径。
- **废纸篓视图**：桶的大小随内容增长（从桌面纸篓到垃圾箱），条目以按安全等级着色的小球堆在桶里；旁边的"来源图"按原始路径重建出这些内容来自哪里，悬停小球、列表行和扇区会互相高亮。条目按删除原因（自动清理、习惯、手动）分组，并给出安全等级分布。
- **恢复与清空**：可逐项或批量恢复；清空前桶会弹出确认气泡，确认后小球逐个消失，释放的空间同步反映在磁盘用量上。
- 兼容旧版本日志中真正移入系统废纸篓的条目（macOS 废纸篓、freedesktop.org Trash、Windows 回收站），仍可恢复或清除。

### 外观与语言

- **十种风格**：HUD、机甲、经典、蓝图、终端、工业、墨水屏、欢乐、粉粉、绿绿。每种风格有自己的扇区画法、仪表层和专属特效（例如墨水屏的刷新闪烁、终端的 CRT 开机和逐字命令行、蓝图先画线稿再上色）。
- **减弱动态效果**选项。
- **多语言**：简体中文、繁体中文、English、日本語，默认跟随系统语言，也可在设置中指定。

### 快捷键（桌面应用）

| 按键 | 作用 |
| --- | --- |
| ↑ / ↓ | 在列表中移动选择 |
| Enter | 打开选中的文件夹 |
| Backspace | 返回上一级 |
| Delete、⌘⌫ | 把选中项移入废纸篓 |
| Esc | 退出分析 / 返回磁盘视图 / 关闭详情 / 返回上一级 |
| ⌘,（Ctrl+,） | 打开设置 |

### 设置

设置分为外观、扫描（默认扫描位置、排除路径、是否跨卷）、清理（评估清单、分析动画、使用中保护）、清理习惯和废纸篓五类，保存在状态目录的 `settings.json` 中。

## 命令行

```
tossy scan  [-w workers] [-x] [-top n] [path]   扫描并列出最大的条目
tossy eval  [-review] [-top n] [path]           扫描后列出可安全清理的内容（只读）
tossy clean [-review] [-yes] [path]             把安全条目（及已学会的习惯）移入废纸篓；不加 -yes 时只演练
tossy rm path...                                手动移入废纸篓（Tossy 会从中学习）
tossy trash [list | restore id... | empty [id...]]
tossy habit [list | forget key]
```

`path` 默认为当前目录。`-review` 会把"需确认"等级的条目也列入。

## 构建与运行

需要 Go 1.27 及以上。

```bash
go build -o bin/tossy ./cmd/tossy
```

```bash
go run github.com/egoist/mygo/cmd/mygo dev ./app
```

```bash
go run github.com/egoist/mygo/cmd/mygo build ./app
```

```bash
go test ./...
```

`mygo dev` 运行带热重载的开发版，`mygo build` 生成正式应用（macOS 上为 .app 和 .dmg）。打包配置见 [app/mygo.json](app/mygo.json)，产物输出到 `app/build/`。

## 数据位置

Tossy 的状态（废纸篓日志、`habits.json`、`settings.json`）保存在系统配置目录下的 `Tossy` 文件夹：

- macOS：`~/Library/Application Support/Tossy`
- Linux：`~/.config/Tossy`
- Windows：`%AppData%\Tossy`

从旧名 FanFree 升级时，首次启动会把原来的 `FanFree` 文件夹改名为 `Tossy`，废纸篓记录、清理习惯和设置都会保留。

可用环境变量 `TOSSY_STATE` 指定其他位置，`TOSSY_THEME` 临时指定风格（如 `TOSSY_THEME=blueprint`），`TOSSY_LANG` 临时指定界面语言。

## 目录结构

```
cmd/tossy        命令行工具
app/             MyGo 桌面应用
engine/scan      并行扫描与紧凑树
engine/safety    安全规则与判定
engine/trash     废纸篓日志、恢复与清空
engine/habit     从手动删除中学习清理习惯
engine/gitrepo   Git 仓库 / 工作树状态检查
engine/statedir  状态目录定位与旧目录迁移
prototype/       HTML 交互原型
```
