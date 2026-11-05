# Linbit 品牌资源（Android 单一来源）

最终版 Logo（设计已确认）。**换 Logo 只需替换这里的 SVG，然后运行：**

```bash
python3 mobile/tools/gen_android_vectors.py
```

脚本把它们转换为 VectorDrawable（`app/src/main/res/drawable/`，仓库内不放 PNG），用于：

| 源文件 | 生成物 | 用途 |
| --- | --- | --- |
| `logo.svg`（#abe127 全幅应用图标，Logo 已按自适应图标 66/108 安全区缩放） | `ic_launcher_foreground` / `ic_launcher_monochrome` / `ic_splash_icon`；背景色 `ic_launcher_background`=#abe127 | 自适应启动图标（含 Android 13 主题图标 monochrome 层）、Android 12+ 系统启动页 |
| `mark.svg`（#070808 符号） | `ic_mark` | 浅色背景上的 Logo、启动动画 |
| `mark-lime.svg`（#abe127 符号） | `ic_mark_lime` | 深色背景上的 Logo、首页头部徽标 |
| `wordmark.svg` / `wordmark-dark.svg` | `ic_wordmark` / `ic_wordmark_dark` | 带字标的横版 Logo（浅色 / 深色背景） |

注意：iOS 的 `mobile/icons/logo.svg`（旧 Logo）由 iOS 自己的 PR 更换，Android 不读取它。
