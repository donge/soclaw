package secops

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/tools/secops"
)

// Service 安全运营服务
type Service struct {
	config          *config.SecOpsConfig
	agentLoop       *agent.AgentLoop
	msgBus          *bus.MessageBus
	queryTool       *secops.SecOpsQueryDataTool
	apiTool         *secops.SecOpsSheikahAPITool
	proposalService *ProposalService
	activities      map[string]*Activity
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// Activity 安全运营活动
type Activity struct {
	Name     string
	Config   *config.ActivityConfig
	stopCh   chan struct{}
}

// NewService 创建安全运营服务
func NewService(cfg *config.SecOpsConfig, agentLoop *agent.AgentLoop, msgBus *bus.MessageBus) (*Service, error) {
	if !cfg.Enabled {
		logger.InfoC("secops", "SecOps service is disabled")
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	svc := &Service{
		config:          cfg,
		agentLoop:       agentLoop,
		msgBus:          msgBus,
		proposalService: NewProposalService(),
		activities:      make(map[string]*Activity),
		ctx:             ctx,
		cancel:          cancel,
	}

	// 初始化工具
	if err := svc.initTools(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to init secops tools: %w", err)
	}

	return svc, nil
}

// ProposalService 获取提案服务
func (s *Service) ProposalService() *ProposalService {
	return s.proposalService
}

// CreateProposal 创建提案
func (s *Service) CreateProposal(proposal *Proposal) string {
	return s.proposalService.Create(proposal)
}

// GetProposal 获取提案
func (s *Service) GetProposal(id string) (*Proposal, bool) {
	return s.proposalService.Get(id)
}

// initTools 初始化安全运营工具
func (s *Service) initTools() error {
	// 初始化 SQL 模板
	queries := map[string]string{
		"pending_risk_events": `SELECT risk, host, content, ts FROM risk_events WHERE status = 'pending' ORDER BY ts DESC LIMIT $batch_size`,
		"pending_weak_events": `SELECT weak_name, host, method, url, channel FROM weak_events WHERE status = 'pending' ORDER BY ts DESC LIMIT $batch_size`,
		"access_by_ip": `SELECT ip, ts, method, url, status, req_risk FROM access WHERE ip = '$ip' AND ts > now() - INTERVAL 1 DAY ORDER BY ts DESC LIMIT 30`,
		"access_by_user": `SELECT ip, ts, method, url, status, req_risk FROM access WHERE uid = '$user_id' AND ts > now() - INTERVAL 1 DAY ORDER BY ts DESC LIMIT 30`,
		"access_by_device": `SELECT ip, ts, method, url, status, req_risk FROM access WHERE sid = '$device_id' AND ts > now() - INTERVAL 1 DAY ORDER BY ts DESC LIMIT 30`,
		"http_details": `SELECT req, res FROM access_raw WHERE id = '$id' LIMIT 3`,
		"risk_top20": `SELECT risk, host, content, type, count() as cnt FROM risk_events WHERE ts > today() AND status = 'pending' GROUP BY risk, host, content, type ORDER BY cnt DESC LIMIT 20`,
		"weak_http_sample": `SELECT req, res FROM weak WHERE weak_name = '$weak_name' AND channel = '$channel' AND method = '$method' AND url = '$url' LIMIT 1`,
		"pending_api_list": `SELECT method, host, url, req, res, biz_type, channel FROM api_sample WHERE analyzed = 0 LIMIT $batch_size`,
		"api_sample": `SELECT method, host, url, req, res FROM api_sample WHERE host = '$host' AND url = '$url' LIMIT 1`,
		"pending_app_list": `SELECT app_id, host, api_list FROM app_sample WHERE analyzed = 0 LIMIT $batch_size`,
		"app_api_list": `SELECT api_list FROM app_sample WHERE app_id = '$app_id' LIMIT 1`,
	}

	// 初始化 ClickHouse 查询工具
	chAddr := s.config.ClickHouse.Addr
	if chAddr == "" {
		chAddr = "localhost:8123"
	}
	chBaseURL := fmt.Sprintf("http://%s", chAddr)
	s.queryTool = secops.NewSecOpsQueryDataTool(
		queries,
		chBaseURL,
		s.config.ClickHouse.Username,
		s.config.ClickHouse.Password,
	)
	s.agentLoop.RegisterTool(s.queryTool)

	// 初始化 API 调用工具
	apis := map[string]secops.APIConfig{
		"confirm_risk": {
			Method: "POST",
			Path:   "/risk/confirm",
			Body:   `[{"content": "$content", "host": "$host", "risk": "$risk", "note": "$note"}]`,
		},
		"ignore_risk": {
			Method: "POST",
			Path:   "/risk/filter",
			Body:   `[{"content": "$content", "host": "$host", "risk": "$risk", "note": "$note"}]`,
		},
		"confirm_weak": {
			Method: "POST",
			Path:   "/apiweak/manage/batch",
			Body:   `{"tag": "todo", "apiWeakMgts": [{"defectId": "$weak_name", "host": "$host", "method": "$method", "url": "$url"}], "message": "$note"}`,
		},
		"ignore_weak": {
			Method: "POST",
			Path:   "/apiweak/manage/batch",
			Body:   `{"tag": "ignore", "apiWeakMgts": [{"defectId": "$weak_name", "host": "$host", "method": "$method", "url": "$url"}], "message": "$note"}`,
		},
		"create_business": {
			Method: "POST",
			Path:   "/antibot/api_data_property",
			Body:   `{"method": "$method", "path": "$path", "host": "$host", "bizType": 0, "bizDesc": "$biz_desc", "bizLevel": $biz_level, "bizName": "$biz_name", "mode": 1, "ruleSet": []}`,
		},
		"save_api_analysis": {
			Method: "POST",
			Path:   "/antibot/internal_api/api_analysis",
			Body:   `{"host": "$host", "method": "$method", "path": "$path", "biz_analysis": "$biz_analysis", "importance_analysis": "$importance_analysis", "param_analysis": "$param_analysis", "importance": "$importance", "skip_if_exist": true}`,
		},
		"create_app": {
			Method: "POST",
			Path:   "/antibot/internal_app",
			Body:   `{"name": "$app_name", "domainList": ["$host"], "urlPrefix": "/", "isMirror": true, "desc": "$app_desc"}`,
		},
		"update_app": {
			Method: "PUT",
			Path:   "/antibot/internal_app/$app_id",
			Body:   `{"desc": "$app_desc"}`,
		},
		"create_proposal": {
			Method: "POST",
			Path:   "/secops/proposal",
			Body:   `{"type": "$type", "title": "$title", "content": "$content", "data": $data}`,
		},
	}

	baseURL := s.config.Sheikah.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	s.apiTool = secops.NewSecOpsSheikahAPITool(apis, baseURL, s.config.Sheikah.APIKey)
	s.agentLoop.RegisterTool(s.apiTool)

	logger.InfoCF("secops", "SecOps tools registered",
		map[string]interface{}{
			"queries_count": len(queries),
			"apis_count":   len(apis),
		})

	return nil
}

// Start 启动安全运营服务
func (s *Service) Start() error {
	if s == nil {
		return nil
	}

	logger.InfoCF("secops", "Starting SecOps service",
		map[string]interface{}{
			"activities": len(s.config.Activities),
		})

	// 启动所有启用的活动
	for name, actCfg := range s.config.Activities {
		if !actCfg.Enabled {
			logger.InfoC("secops", fmt.Sprintf("Activity %s is disabled", name))
			continue
		}

		activity := &Activity{
			Name:   name,
			Config: &actCfg,
			stopCh: make(chan struct{}),
		}
		s.activities[name] = activity

		s.wg.Add(1)
		go s.runActivity(activity)
	}

	return nil
}

// runActivity 运行单个活动
func (s *Service) runActivity(activity *Activity) {
	defer s.wg.Done()

	// 解析调度间隔
	interval := s.parseSchedule(activity.Config.Schedule)
	if interval <= 0 {
		interval = 30 * time.Minute // 默认30分钟
	}

	logger.InfoCF("secops", fmt.Sprintf("Activity %s started with interval %v", activity.Name, interval),
		map[string]interface{}{
			"mode": activity.Config.Mode,
		})

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 立即执行一次
	s.executeActivity(activity.Name)

	for {
		select {
		case <-ticker.C:
			s.executeActivity(activity.Name)
		case <-activity.stopCh:
			logger.InfoC("secops", fmt.Sprintf("Activity %s stopped", activity.Name))
			return
		case <-s.ctx.Done():
			return
		}
	}
}

// parseSchedule 解析调度表达式
func (s *Service) parseSchedule(schedule string) time.Duration {
	// 简单解析：支持 "*/30 * * * *" 格式的 cron 和 "30m" 格式的间隔
	if schedule == "" {
		return 0
	}

	// 支持简单的间隔格式: "30m", "1h", "60s"
	switch {
	case len(schedule) >= 2 && schedule[len(schedule)-1] == 'm':
		var mins int
		fmt.Sscanf(schedule[:len(schedule)-1], "%d", &mins)
		return time.Duration(mins) * time.Minute
	case len(schedule) >= 2 && schedule[len(schedule)-1] == 'h':
		var hours int
		fmt.Sscanf(schedule[:len(schedule)-1], "%d", &hours)
		return time.Duration(hours) * time.Hour
	case len(schedule) >= 2 && schedule[len(schedule)-1] == 's':
		var secs int
		fmt.Sscanf(schedule[:len(schedule)-1], "%d", &secs)
		return time.Duration(secs) * time.Second
	}

	// 默认30分钟
	return 30 * time.Minute
}

// executeActivity 执行活动
func (s *Service) executeActivity(activityName string) {
	logger.InfoC("secops", fmt.Sprintf("Executing activity: %s", activityName))

	// 构建执行 prompt
	prompt := s.buildActivityPrompt(activityName)

	// 使用 agent loop 执行
	channel := "secops"
	chatID := activityName

	_, err := s.agentLoop.ProcessHeartbeat(s.ctx, prompt, channel, chatID)
	if err != nil {
		logger.ErrorC("secops", fmt.Sprintf("Activity %s failed: %v", activityName, err))
		return
	}

	logger.InfoC("secops", fmt.Sprintf("Activity %s completed", activityName))
}

// buildActivityPrompt 构建活动执行 prompt
func (s *Service) buildActivityPrompt(activityName string) string {
	switch activityName {
	case "risk_analysis":
		return `请执行风险事件研判分析：
1. 使用 query_data 工具查询待处理风险事件 (sql_id: pending_risk_events, params: batch_size=5)
2. 对每个风险事件进行溯源分析，查询相关访问记录和HTTP报文
3. 分析事件是否真实存在风险
4. 根据配置模式 (auto/manual) 执行确认或忽略操作

请开始执行风险研判分析。`

	case "weak_analysis":
		return `请执行弱点事件分析：
1. 使用 query_data 工具查询待处理弱点事件 (sql_id: pending_weak_events, params: batch_size=5)
2. 获取弱点触发时的HTTP流量详情
3. 分析是否为误报
4. 根据配置模式 (auto/manual) 执行确认或忽略操作

请开始执行弱点分析。`

	case "api_biz_explain":
		return `请执行API业务分析：
1. 使用 query_data 工具查询待分析API列表 (sql_id: pending_api_list, params: batch_size=3)
2. 获取API的HTTP请求和响应样本
3. 分析API的业务含义、参数、重要性等级
4. 创建业务并配置防护策略

请开始执行API业务分析。`

	case "app_explain":
		return `请执行应用系统识别：
1. 使用 query_data 工具查询待识别应用列表 (sql_id: pending_app_list, params: batch_size=3)
2. 获取应用的API列表
3. 分析应用名称和业务描述
4. 创建或更新应用配置

请开始执行应用识别。`

	default:
		return fmt.Sprintf(`请执行安全运营活动: %s`, activityName)
	}
}

// Stop 停止安全运营服务
func (s *Service) Stop() {
	if s == nil {
		return
	}

	logger.InfoC("secops", "Stopping SecOps service")

	s.cancel()

	// 停止所有活动
	for _, activity := range s.activities {
		close(activity.stopCh)
	}

	s.wg.Wait()

	// 关闭工具
	if s.queryTool != nil {
		s.queryTool.Close()
	}
	if s.apiTool != nil {
		s.apiTool.Close()
	}

	logger.InfoC("secops", "SecOps service stopped")
}
