package secops

import "time"

// Proposal 提案结构
type Proposal struct {
	ID         string                 // 提案ID
	Type       string                 // 提案类型: risk, weak, api_biz, app
	Title      string                 // 提案标题
	Summary    string                 // 简要总结
	Details    map[string]interface{} // 详细数据
	Actions    []ProposalAction      // 可选操作
	Parameters map[string]Param       // 可调整参数
	Status     ProposalStatus         // 提案状态
	CreatedAt  time.Time              // 创建时间
	UpdatedAt  time.Time              // 更新时间
}

// ProposalAction 可选操作
type ProposalAction struct {
	Label  string            // 按钮文字: "确认风险", "忽略", "修改参数"
	Type   string           // accept, ignore, modify
	Params map[string]string // 操作参数
}

// Param 可调整参数
type Param struct {
	Key     string   // 参数名
	Label   string   // 显示标签
	Type    string   // string, number, select
	Value   string   // 当前值
	Options []string // 可选值 (for select)
}

// ProposalStatus 提案状态
type ProposalStatus string

const (
	ProposalStatusPending ProposalStatus = "pending"
	ProposalStatusAccepted ProposalStatus = "accepted"
	ProposalStatusIgnored  ProposalStatus = "ignored"
	ProposalStatusModified ProposalStatus = "modified"
)

// NewProposal 创建新提案
func NewProposal(proposalType, title, summary string, details map[string]interface{}) *Proposal {
	return &Proposal{
		Type:       proposalType,
		Title:      title,
		Summary:    summary,
		Details:    details,
		Actions:    []ProposalAction{},
		Parameters: make(map[string]Param),
		Status:     ProposalStatusPending,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}
