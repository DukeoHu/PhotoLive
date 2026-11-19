# PhotoLive

面向摄影师与旅行团队的即时照片直播平台。摄影师在现场上传照片，客户扫码进入房间即可实时浏览、挑片，摄影师直接拿到选中清单。

## 为什么做这个

传统流程是：拍摄 → 拷贝到电脑 → 上传网盘 → 发链接 → 微信沟通“第几张要修”。客户全程在等，摄影师还要反复确认。

PhotoLive 把这条链路压缩成：**现场上传 → 客户扫码看 → 点选 → 导出清单**。客户不需要安装 App、不需要注册，输入 6 位访问码即可开始挑片。

## 功能

**摄影师端**

- 邮箱密码注册登录，连续失败 5 次锁定 15 分钟
- 创建活动房间，自动生成访问码、分享链接与二维码
- 访问码可自定义、可重置；重置后已进入的客户不会被强制踢出
- 批量上传（默认单次 300 张），支持拖拽、进度、失败重试
- 自动读取 EXIF 拍摄时间与方向，生成 480 / 1600 两档缩略图
- 支持 JPG / PNG / WebP / HEIC（HEIC 自动转 JPG 后入库）
- 实时照片墙、正倒序切换、按时间筛选、删除照片
- 查看客户选中结果，CSV / TXT 导出带时间戳
- 批量打包下载已选原图，服务端后台打包，关闭页面后继续
- 结束活动后客户只读；30 天后自动清理照片文件，保留房间与选中记录

**客户端（免注册）**

- 访问码进入房间，手机 H5 自适应
- 3 秒轮询，新照片自动出现并提示
- 点选 / 取消选中，支持“仅看已选”
- 全屏预览，左右滑动切换
- 下载已选原图 ZIP

**访客上传**

- 房间可开启“允许访客上传”，适合旅行同行互传
- 可再开启“访客照片需审核”，摄影师通过后才进入公开相册
- 访客照片带角标，摄影师可单独删除

## 技术栈

- 后端：Go（标准库 `net/http`，单二进制）
- 数据库：SQLite（默认）/ MySQL，启动时自动建表与补列
- 存储：本地文件系统，预留 OSS 接口抽象
- 前端：Tailwind CSS + 原生 JavaScript，无构建步骤
- 图片处理：`disintegration/imaging`、`goexif`、`go-qrcode`

## 快速开始

环境要求：Go 1.27+；如需支持 HEIC，另需安装 `ffmpeg`、ImageMagick、`heif-convert` 或 `sips` 之一。

```bash
git clone <repo-url> photolive
cd photolive
go run .
```

启动后终端会打印局域网访问地址，例如：

```
PhotoLive 已启动: http://192.168.1.10:8080（监听 0.0.0.0:8080，局域网内手机可直接访问）
```

打开该地址 → `/login` 注册账号 → 创建房间 → 上传照片。手机连接同一 Wi-Fi 后扫描房间二维码即可访问。

也可以直接构建：

```bash
go build -o photolive .
./photolive
```

## 配置

全部通过环境变量配置，未设置时使用本地开发默认值。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PHOTOLIVE_ADDR` | `0.0.0.0:8080` | 监听地址 |
| `PHOTOLIVE_DB_DRIVER` | `sqlite` | `sqlite` 或 `mysql` |
| `PHOTOLIVE_DB_DSN` | 自动生成于 `data/photolive.db` | SQLite 路径或 MySQL DSN |
| `PHOTOLIVE_STORAGE` | `local` | 存储驱动，目前仅 `local` |
| `PHOTOLIVE_STORAGE_DIR` | `./data/media` | 本地存储目录 |
| `PHOTOLIVE_BASE_URL` | 自动探测局域网 IP | 对外地址，反向代理/域名部署时必填 |
| `PHOTOLIVE_SECRET` | 自动生成 `data/secret.key` | 会话与访问凭证签名密钥 |
| `PHOTOLIVE_MAX_UPLOAD_MB` | `50` | 单张照片大小上限 |
| `PHOTOLIVE_MAX_FILES` | `300` | 单次批量上传上限 |
| `PHOTOLIVE_MAX_ROOM_PHOTOS` | `3000` | 单个房间照片上限 |
| `PHOTOLIVE_RETENTION_DAYS` | `30` | 活动结束后的照片保留天数 |
| `PHOTOLIVE_HEIC_CONVERTER` | 自动探测 | 指定 HEIC 转换器路径 |

MySQL 示例：

```bash
PHOTOLIVE_DB_DRIVER=mysql \
PHOTOLIVE_DB_DSN='user:pass@tcp(127.0.0.1:3306)/photolive?parseTime=true&charset=utf8mb4' \
./photolive
```

数据库表结构会在启动时自动创建，升级版本时也会自动补齐新增字段。

## 生产部署提示

- 使用 HTTPS，并设置 `PHOTOLIVE_BASE_URL=https://your-domain`，否则二维码里的地址可能是内网 IP
- 反向代理的请求体上限至少 200MB，需大于单张照片上限
- 持久化 `data/` 目录（数据库、密钥、照片文件）
- 修改或固定 `PHOTOLIVE_SECRET`，避免重启后会话失效
- 多实例部署时需共享数据库与存储；后台打包任务当前为单实例内存态

## 目录结构

```
main.go                 启动入口
internal/config         环境变量与密钥
internal/store          数据库模型、迁移与查询（SQLite / MySQL）
internal/server         HTTP 路由、页面与 API
internal/media          图片压缩、EXIF、HEIC 转换
internal/storage        存储抽象（local / OSS 预留）
internal/diskfree       磁盘空间检查
web/templates           HTML 模板
web/static              CSS / JavaScript
data/                   运行期数据（已 gitignore）
PhotoLive-PRD.html      产品需求文档与可交互原型
UI设计.txt              视觉设计说明
```

## 已知限制

- HEIC 依赖外部转换工具，未安装时上传会返回明确错误
- OSS 驱动仅有接口占位，尚未实现
- 后台打包任务保存在内存中，服务重启后未取回的任务需重新打包
- 客户完全免注册，因此换设备或清除浏览器缓存后需要重新挑片
- 未实现密码找回，账号密码由部署者自行管理

## 文档

- [产品需求文档](PhotoLive-PRD.html)：包含角色、流程、功能需求与可交互原型
- [UI 设计说明](UI设计.txt)：配色、字体与布局规范

## License

[MIT](LICENSE)
