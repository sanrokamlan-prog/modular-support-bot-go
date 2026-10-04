# Modular Support Bot Go

这是一个给自己部署的 Telegram 双向客服工单机器人：

- 用户第一次私聊必须先完成数学题验证
- 验证状态、失败次数和锁定时间持久化到该 Bot 自己的 SQLite 文件
- 每个工单在客服超级群组中创建独立 Forum Topic
- 用户消息转发到对应 Topic，客服直接在 Topic 中回复用户
- 失败次数锁定、用户消息限流、`/close` 和 `/ban`
- Docker Compose 部署，不依赖 MongoDB

## Telegram 配置

1. 用 BotFather 创建机器人并拿到 token。
2. 创建一个 Telegram **超级群组**，开启 Topics/论坛话题。
3. 把机器人加入该群并授予管理员权限，至少允许：
   - 管理话题
   - 发送消息
   - 删除消息（可选）
4. 关闭 BotFather 的 Group Privacy，否则机器人收不到客服 Topic 中的普通回复。
5. 把客服人员的 Telegram ID 写入 `STAFF_IDS`。只有这些账号能把 Topic 消息转给用户。

## VPS 部署

```bash
cp .env.example .env
# 编辑 .env，填 BOT_TOKEN、STAFF_CHAT_ID、STAFF_IDS
docker compose up -d --build
docker compose logs -f support-bot
```

`STAFF_CHAT_ID` 必须是超级群组 ID，通常形如 `-100...`。数据库保存在 `./data/support.db`，备份这个文件即可备份工单和验证状态。

## VPS 推荐配置

个人使用、几百到几千名偶尔联系的用户：

- 1 vCPU
- 1 GB RAM
- 20 GB SSD
- Debian 12 或 Ubuntu 24.04

如果还要在同一台 VPS 上运行网站、数据库或多个机器人，建议使用 2 vCPU / 2 GB RAM。这个服务主要是 Go + SQLite，CPU 和内存占用都很低，真正需要留余量的是 Docker、日志和数据库备份。

## 当前边界

这是可运行的核心版本，先解决机器人群发和工单串线问题。当前用户侧优先支持文字消息；照片/文件、分类路由、后台网页面板和更细的客服权限可以在这个基础上继续加。数学题不是绝对的反自动化保证，但多步算式、失败锁定、限流和人工封禁组合后，能挡住大多数脚本批量开单。
