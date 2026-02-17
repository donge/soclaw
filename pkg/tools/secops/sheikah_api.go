package secops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sipeed/picoclaw/pkg/tools"
)

// SheikahAPITool 调用内部 API
type SecOpsSheikahAPITool struct {
	apis   map[string]APIConfig
	baseURL string
	apiKey  string
	client  *http.Client
}

// APIConfig API 端点配置
type APIConfig struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   string `json:"body,omitempty"`
}

// NewSecOpsSheikahAPITool 创建 API 调用工具
func NewSecOpsSheikahAPITool(apis map[string]APIConfig, baseURL, apiKey string) *SecOpsSheikahAPITool {
	return &SecOpsSheikahAPITool{
		apis:    apis,
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

// Name 工具名称
func (t *SecOpsSheikahAPITool) Name() string {
	return "sheikah_api"
}

// Description 工具描述
func (t *SecOpsSheikahAPITool) Description() string {
	apiList := make([]string, 0, len(t.apis))
	for id := range t.apis {
		apiList = append(apiList, id)
	}
	return fmt.Sprintf(`调用内部 Sheikah API 进行处置操作。使用方法:
- api: API 标识 (如 %s)
- params: 参数替换, 格式为 key1=value1,key2=value2

示例:
sheikah_api --api confirm_risk --params content=xxx,host=xxx,risk=xxx
sheikah_api --api create_proposal --params type=risk,data=xxx`, strings.Join(apiList, ", "))
}

// Parameters 参数定义
func (t *SecOpsSheikahAPITool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"api": map[string]interface{}{
				"type":        "string",
				"description": "API 标识",
			},
			"params": map[string]interface{}{
				"type":        "string",
				"description": "参数替换, 格式: key1=value1,key2=value2",
			},
		},
		"required": []string{"api"},
	}
}

// Execute 执行 API 调用
func (t *SecOpsSheikahAPITool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	apiID, _ := args["api"].(string)
	paramsStr, _ := args["params"].(string)

	if apiID == "" {
		return tools.ErrorResult("api is required")
	}

	apiConfig, ok := t.apis[apiID]
	if !ok {
		return tools.ErrorResult(fmt.Sprintf("api not found: %s", apiID))
	}

	// 替换参数
	body := t.replaceParams(apiConfig.Body, paramsStr)

	// 构建请求
	url := t.baseURL + apiConfig.Path
	var reqBody io.Reader
	if body != "" {
		reqBody = bytes.NewBufferString(body)
	}

	req, err := http.NewRequestWithContext(ctx, apiConfig.Method, url, reqBody)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to create request: %v", err))
	}

	req.Header.Set("Content-Type", "application/json")
	if t.apiKey != "" {
		req.Header.Set("sw-api-key", t.apiKey)
	}

	// 发送请求
	resp, err := t.client.Do(req)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("request failed: %v", err))
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to read response: %v", err))
	}

	if resp.StatusCode >= 400 {
		return tools.ErrorResult(fmt.Sprintf("API returned error: %d - %s", resp.StatusCode, string(respBody)))
	}

	// 尝试解析 JSON 响应
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, respBody, "", "  "); err == nil {
		return tools.UserResult(prettyJSON.String())
	}

	return tools.UserResult(string(respBody))
}

// replaceParams 替换参数
func (t *SecOpsSheikahAPITool) replaceParams(template, paramsStr string) string {
	if template == "" || paramsStr == "" {
		return template
	}

	params := make(map[string]string)
	pairs := strings.Split(paramsStr, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			params[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}

	result := template
	for k, v := range params {
		result = strings.ReplaceAll(result, "{{."+k+"}}", v)
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
		result = strings.ReplaceAll(result, "$"+k, v)
	}

	return result
}

// Close 关闭客户端
func (t *SecOpsSheikahAPITool) Close() error {
	t.client = nil
	return nil
}
