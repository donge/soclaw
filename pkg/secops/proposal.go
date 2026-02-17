package secops

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sipeed/picoclaw/pkg/logger"
)

// ProposalService 提案服务
type ProposalService struct {
	proposals map[string]*Proposal
	channel   chan *Proposal // 新提案通知
	mu        sync.RWMutex
}

// NewProposalService 创建提案服务
func NewProposalService() *ProposalService {
	return &ProposalService{
		proposals: make(map[string]*Proposal),
		channel:   make(chan *Proposal, 10),
	}
}

// Create 创建提案
func (s *ProposalService) Create(proposal *Proposal) string {
	if proposal.ID == "" {
		proposal.ID = uuid.New().String()
	}
	if proposal.CreatedAt.IsZero() {
		proposal.CreatedAt = time.Now()
	}
	proposal.UpdatedAt = time.Now()

	s.mu.Lock()
	s.proposals[proposal.ID] = proposal
	s.mu.Unlock()

	logger.InfoCF("secops", "Proposal created",
		map[string]interface{}{
			"id":    proposal.ID,
			"type":  proposal.Type,
			"title": proposal.Title,
		})

	// 通知新提案
	select {
	case s.channel <- proposal:
	default:
		logger.WarnC("secops", "Proposal channel full, notification skipped")
	}

	return proposal.ID
}

// Get 获取提案
func (s *ProposalService) Get(id string) (*Proposal, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.proposals[id]
	return p, ok
}

// GetAll 获取所有提案
func (s *ProposalService) GetAll() []*Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Proposal, 0, len(s.proposals))
	for _, p := range s.proposals {
		result = append(result, p)
	}
	return result
}

// GetPending 获取待处理的提案
func (s *ProposalService) GetPending() []*Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Proposal, 0)
	for _, p := range s.proposals {
		if p.Status == ProposalStatusPending {
			result = append(result, p)
		}
	}
	return result
}

// Accept 接受提案
func (s *ProposalService) Accept(id string, params map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.proposals[id]
	if !ok {
		return fmt.Errorf("proposal not found: %s", id)
	}

	if p.Status != ProposalStatusPending {
		return fmt.Errorf("proposal already processed: %s", p.Status)
	}

	p.Status = ProposalStatusAccepted
	p.UpdatedAt = time.Now()

	logger.InfoCF("secops", "Proposal accepted",
		map[string]interface{}{
			"id":     p.ID,
			"type":   p.Type,
			"title":  p.Title,
			"params": params,
		})

	return nil
}

// Ignore 忽略提案
func (s *ProposalService) Ignore(id string, params map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.proposals[id]
	if !ok {
		return fmt.Errorf("proposal not found: %s", id)
	}

	if p.Status != ProposalStatusPending {
		return fmt.Errorf("proposal already processed: %s", p.Status)
	}

	p.Status = ProposalStatusIgnored
	p.UpdatedAt = time.Now()

	logger.InfoCF("secops", "Proposal ignored",
		map[string]interface{}{
			"id":     p.ID,
			"type":   p.Type,
			"title":  p.Title,
			"params": params,
		})

	return nil
}

// Resubmit 重新分析 - 使用修改后的参数
func (s *ProposalService) Resubmit(id string, params map[string]string) (*Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.proposals[id]
	if !ok {
		return nil, fmt.Errorf("proposal not found: %s", id)
	}

	// 更新参数
	for key, value := range params {
		if param, exists := p.Parameters[key]; exists {
			param.Value = value
			p.Parameters[key] = param
		}
	}

	p.Status = ProposalStatusModified
	p.UpdatedAt = time.Now()

	logger.InfoCF("secops", "Proposal resubmitted with modified params",
		map[string]interface{}{
			"id":     p.ID,
			"type":   p.Type,
			"title":  p.Title,
			"params": params,
		})

	return p, nil
}

// Channel 获取提案通知通道
func (s *ProposalService) Channel() <-chan *Proposal {
	return s.channel
}

// Delete 删除提案
func (s *ProposalService) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.proposals[id]; ok {
		delete(s.proposals, id)
		return true
	}
	return false
}
