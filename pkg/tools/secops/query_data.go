package secops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sipeed/picoclaw/pkg/tools"
)

// SecOpsQueryDataTool 从 ClickHouse 查询数据（通过 HTTP API）
type SecOpsQueryDataTool struct {
	queries  map[string]string
	baseURL  string
	username string
	password string
	client   *http.Client
}

// NewSecOpsQueryDataTool 创建查询数据工具
func NewSecOpsQueryDataTool(queries map[string]string, baseURL, username, password string) *SecOpsQueryDataTool {
	return &SecOpsQueryDataTool{
		queries:  queries,
		baseURL:  baseURL,
		username: username,
		password: password,
		client:   &http.Client{},
	}
}

// Name 工具名称
func (t *SecOpsQueryDataTool) Name() string {
	return "query_data"
}

// Description 工具描述
func (t *SecOpsQueryDataTool) Description() string {
	// 获取可用的 sql_id 列表
	var ids []string
	for id := range t.queries {
		ids = append(ids, id)
	}
	return fmt.Sprintf(`从 ClickHouse 查询数据。使用方法:
- sql_id: SQL 模板 ID (如: %s)
- params: 参数替换, 格式为 key1=value1,key2=value2
- raw_sql: 可选, 直接执行的 SQL (优先级高于 sql_id)

可用 SQL 模板: %s`, strings.Join(ids, ", "), strings.Join(ids, ", "))
}

// Parameters 参数定义
func (t *SecOpsQueryDataTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"sql_id": map[string]interface{}{
				"type":        "string",
				"description": "SQL 模板 ID",
			},
			"params": map[string]interface{}{
				"type":        "string",
				"description": "参数替换, 格式: key1=value1,key2=value2",
			},
			"raw_sql": map[string]interface{}{
				"type":        "string",
				"description": "可选, 直接执行的 SQL",
			},
		},
	}
}

// Execute 执行查询
func (t *SecOpsQueryDataTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	sqlID, _ := args["sql_id"].(string)
	paramsStr, _ := args["params"].(string)
	rawSQL, _ := args["raw_sql"].(string)

	var sql string

	if rawSQL != "" {
		sql = rawSQL
	} else if sqlID != "" {
		template, ok := t.queries[sqlID]
		if !ok {
			return tools.ErrorResult(fmt.Sprintf("sql_id not found: %s. Available: %v", sqlID, t.queries))
		}
		sql = t.replaceParams(template, paramsStr)
	} else {
		return tools.ErrorResult("sql_id or raw_sql is required")
	}

	// 构建 HTTP 请求
	form := url.Values{}
	form.Set("query", sql)
	if t.username != "" {
		form.Set("user", t.username)
	}
	if t.password != "" {
		form.Set("password", t.password)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to create request: %v", err))
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client.Do(req)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("request failed: %v", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to read response: %v", err))
	}

	if resp.StatusCode >= 400 {
		return tools.ErrorResult(fmt.Sprintf("ClickHouse error %d: %s", resp.StatusCode, string(body)))
	}

	// 解析 JSON 响应
	var result struct {
		Data [][]interface{} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		// 如果不是 JSON，直接返回原始响应
		return tools.UserResult(string(body))
	}

	// 格式化输出
	if len(result.Data) == 0 {
		return tools.UserResult("查询结果为空")
	}

	var output strings.Builder
	// TODO: 获取列名并输出表头
	output.WriteString(fmt.Sprintf("共 %d 条结果:\n\n", len(result.Data)))

	// 输出前10条
	maxRows := 10
	if len(result.Data) < maxRows {
		maxRows = len(result.Data)
	}

	for i := 0; i < maxRows; i++ {
		var rowStrs []string
		for _, v := range result.Data[i] {
			if v == nil {
				rowStrs = append(rowStrs, "NULL")
			} else {
				rowStrs = append(rowStrs, fmt.Sprintf("%v", v))
			}
		}
		output.WriteString(strings.Join(rowStrs, "\t"))
		output.WriteString("\n")
	}

	if len(result.Data) > maxRows {
		output.WriteString(fmt.Sprintf("\n... 还有 %d 条结果", len(result.Data)-maxRows))
	}

	return tools.UserResult(output.String())
}

// replaceParams 替换 SQL 参数
func (t *SecOpsQueryDataTool) replaceParams(template, paramsStr string) string {
	if paramsStr == "" {
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
func (t *SecOpsQueryDataTool) Close() error {
	t.client = nil
	return nil
}

// Query 执行原始 SQL（供其他工具使用）
func (t *SecOpsQueryDataTool) Query(ctx context.Context, sql string) ([][]interface{}, error) {
	form := url.Values{}
	form.Set("query", sql)
	if t.username != "" {
		form.Set("user", t.username)
	}
	if t.password != "" {
		form.Set("password", t.password)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ClickHouse error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data [][]interface{} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}
