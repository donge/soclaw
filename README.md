# 安全运营龙虾 (Security Operation Claw)

基于 [PicoClaw](https://github.com/sipeed/picoclaw) 的智能安全运营助手

<div align="center">
  <img src="assets/logo.jpg" alt="Security Operation Claw" width="256">
  <h1>🦞 安全运营龙虾</h1>
  <h3>智能安全运营自动化平台</h3>
</div>

---

## 特性

### 🔍 风险事件研判
- 自动查询待处理风险事件
- 溯源分析访问记录和HTTP报文
- 智能判断风险真实性

### ⚠️ 弱点事件分析
- 分析弱点触发时的HTTP流量
- 自动识别误报
- 确认真实安全问题

### 🔗 API业务识别
- 自动分析API的业务含义
- 识别参数和重要性等级
- 辅助配置防护策略

### 📱 应用系统识别
- 根据API列表自动识别应用名称
- 管理应用配置

### 💬 多渠道接入
- 支持 Telegram、Discord、Slack、DingTalk、QQ 等
- Web Debug UI 可视化操作

---

## 快速开始

### Docker 部署 (推荐)

```bash
# 1. 克隆项目
git clone https://github.com/donge/soclaw.git
cd soclaw

# 2. 复制配置
cp docker-compose.example.yml docker-compose.yml

# 3. 编辑配置
vim docker-compose.yml
# 添加你的 API Key 等

# 4. 启动
docker-compose up -d

# 5. 访问
# Gateway: http://localhost:18790
# Debug UI: http://localhost:18789
```

### 本地运行

```bash
# 1. 克隆项目
git clone https://github.com/donge/soclaw.git
cd soclaw

# 2. 构建
make build

# 3. 初始化配置
./build/picoclaw onboard

# 4. 编辑配置
vim ~/.picoclaw/config.json

# 5. 启动
./build/picoclaw gateway
```

---

## 配置说明

### 基本配置

```json
{
  "agents": {
    "defaults": {
      "workspace": "~/.picoclaw/workspace",
      "model": "glm-4-flash",
      "provider": "openrouter"
    }
  },
  "providers": {
    "openrouter": {
      "api_key": "your_api_key"
    }
  }
}
```

### 安全运营配置

```json
{
  "secops": {
    "enabled": true,
    "clickhouse": {
      "addr": "localhost:8123",
      "database": "secops",
      "username": "default",
      "password": ""
    },
    "sheikah": {
      "base_url": "http://localhost:8080",
      "api_key": ""
    },
    "activities": {
      "risk_analysis": {
        "enabled": true,
        "schedule": "30m",
        "mode": "manual"
      },
      "weak_analysis": {
        "enabled": true,
        "schedule": "60m",
        "mode": "auto"
      }
    },
    "debugui": {
      "enabled": true,
      "host": "0.0.0.0",
      "port": 18789
    }
  }
}
```

### 环境变量

| 变量 | 说明 |
|------|------|
| `PICOCLAW_SECOPS_ENABLED` | 启用安全运营 |
| `PICOCLAW_DEBUGUI_ENABLED` | 启用Debug UI |
| `PICOCLAW_DEBUGUI_PORT` | Debug UI 端口 |

---

## 工作流

### 手动模式 (Manual)
```
定时任务 → LLM分析 → 生成提案 → 用户审批 → 执行操作
```

### 自动模式 (Auto)
```
定时任务 → LLM分析 → 自动确认/忽略
```

---

## Debug UI

访问 http://localhost:18789

- 💬 **对话** - 与Agent实时交流
- 🔧 **工具** - 查看可用工具
- ✨ **技能** - 查看已加载技能
- 📋 **提案** - 审批安全运营提案
- ⚙️ **设置** - 查看系统信息

---

## 目录结构

```
soclaw/
├── pkg/
│   ├── debugui/          # Debug UI 服务器
│   ├── secops/          # 安全运营服务
│   │   ├── service.go  # 主服务
│   │   ├── proposal.go  # 提案服务
│   │   └── types.go     # 数据结构
│   └── tools/secops/    # 安全运营工具
│       ├── query_data.go
│       └── sheikah_api.go
├── workspace/skills/
│   └── secops/          # 安全运营技能
│       ├── SKILL.md
│       └── references/
├── docs/
│   ├── index.html       # 特性介绍页
│   └── secops_design.md # 设计文档
├── Dockerfile
└── docker-compose.example.yml
```

---

## License

MIT License
