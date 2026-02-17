package debugui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/secops"
)

// Server Debug UI 服务器
type Server struct {
	addr            string
	agentLoop       *agent.AgentLoop
	proposalService *secops.ProposalService
	secopsService   *secops.Service
	workspace       string
	mu              sync.RWMutex
	server          *http.Server
}

// NewServer 创建 Debug UI 服务器
func NewServer(addr string, agentLoop *agent.AgentLoop, proposalService *secops.ProposalService, secopsService *secops.Service, workspace string) *Server {
	return &Server{
		addr:            addr,
		agentLoop:       agentLoop,
		proposalService: proposalService,
		secopsService:   secopsService,
		workspace:       workspace,
	}
}

// SetAgentLoop 设置 agent loop
func (s *Server) SetAgentLoop(agentLoop *agent.AgentLoop) {
	s.agentLoop = agentLoop
}

// Start 启动服务器
func (s *Server) Start() error {
	if s.addr == "" {
		s.addr = ":18789"
	}

	mux := http.NewServeMux()

	// API 路由 - Agent
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.HandleFunc("/api/tools", s.handleTools)
	mux.HandleFunc("/api/skills", s.handleSkills)
	mux.HandleFunc("/api/info", s.handleInfo)

	// API 路由 - Proposals
	mux.HandleFunc("/api/proposals", s.handleProposals)
	mux.HandleFunc("/api/proposal/", s.handleProposal)
	mux.HandleFunc("/api/proposal/{id}/accept", s.handleAccept)
	mux.HandleFunc("/api/proposal/{id}/ignore", s.handleIgnore)
	mux.HandleFunc("/api/proposal/{id}/resubmit", s.handleResubmit)

	// 前端页面
	mux.HandleFunc("/", s.handleIndex)

	s.server = &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	logger.InfoCF("debugui", "Starting Debug UI server",
		map[string]interface{}{
			"addr": s.addr,
		})

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("debugui server error: %w", err)
	}

	return nil
}

// Stop 停止服务器
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// handleChat 处理聊天请求
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.agentLoop == nil {
		http.Error(w, "agent not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Message string `json:"message"`
		Session string `json:"session"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	if req.Session == "" {
		req.Session = "debugui"
	}

	ctx := context.Background()
	response, err := s.agentLoop.ProcessDirect(ctx, req.Message, "debugui:"+req.Session)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"response": response,
	})
}

// handleTools 获取工具列表
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.agentLoop == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tools": []string{},
		})
		return
	}

	startupInfo := s.agentLoop.GetStartupInfo()
	toolsInfo := startupInfo["tools"].(map[string]interface{})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"tools": toolsInfo["names"],
	})
}

// handleSkills 获取技能列表
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	type skillDetail struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Source      string `json:"source"`
	}

	skills := make([]skillDetail, 0)

	// 读取 workspace 下的 skills 目录
	if s.workspace != "" {
		homeDir, _ := os.UserHomeDir()
		skillsDirs := []struct {
			dir    string
			source string
		}{
			{filepath.Join(s.workspace, "skills"), "workspace"},
			{filepath.Join(homeDir, ".picoclaw", "skills"), "global"},
		}

		for _, sd := range skillsDirs {
			if dirs, err := os.ReadDir(sd.dir); err == nil {
				for _, dir := range dirs {
					if dir.IsDir() {
						skillFile := filepath.Join(sd.dir, dir.Name(), "SKILL.md")
						if _, err := os.Stat(skillFile); err == nil {
							desc := ""
							if data, err := os.ReadFile(skillFile); err == nil {
								// 读取 SKILL.md 的第一行作为描述
								lines := strings.Split(string(data), "\n")
								for _, line := range lines {
									line = strings.TrimSpace(line)
									if strings.HasPrefix(line, "description:") {
										desc = strings.TrimPrefix(line, "description:")
										desc = strings.TrimSpace(desc)
										break
									}
								}
								if desc == "" && len(lines) > 1 {
									// 如果没有 description，使用第二行
									desc = strings.TrimSpace(lines[1])
									if len(desc) > 100 {
										desc = desc[:100] + "..."
									}
								}
							}
							skills = append(skills, skillDetail{
								Name:        dir.Name(),
								Description: desc,
								Source:      sd.source,
							})
						}
					}
				}
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"skills":  skills,
		"total":   len(skills),
		"count":   len(skills),
	})
}

// handleInfo 获取系统信息
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	info := map[string]interface{}{
		"version": "dev",
	}

	if s.agentLoop != nil {
		startupInfo := s.agentLoop.GetStartupInfo()
		info["agent"] = startupInfo
	}

	json.NewEncoder(w).Encode(info)
}

// handleProposals 获取所有提案
func (s *Server) handleProposals(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.proposalService == nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}

	proposals := s.proposalService.GetAll()

	type proposalJSON struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Title      string `json:"title"`
		Summary    string `json:"summary"`
		Status     string `json:"status"`
		CreatedAt  string `json:"createdAt"`
		UpdatedAt  string `json:"updatedAt"`
	}

	result := make([]proposalJSON, len(proposals))
	for i, p := range proposals {
		result[i] = proposalJSON{
			ID:        p.ID,
			Type:      p.Type,
			Title:     p.Title,
			Summary:   p.Summary,
			Status:    string(p.Status),
			CreatedAt: p.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: p.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	json.NewEncoder(w).Encode(result)
}

// handleProposal 获取单个提案详情
func (s *Server) handleProposal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id := r.URL.Path[len("/api/proposal/"):]
	if id == "" {
		http.Error(w, "proposal id required", http.StatusBadRequest)
		return
	}

	if s.proposalService == nil {
		http.Error(w, "proposal service not available", http.StatusServiceUnavailable)
		return
	}

	proposal, ok := s.proposalService.Get(id)
	if !ok {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(proposal)
}

// handleAccept 接受提案
func (s *Server) handleAccept(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id := r.URL.Path[len("/api/proposal/"):]
	id = id[:len(id)-len("/accept")]

	if id == "" {
		http.Error(w, "proposal id required", http.StatusBadRequest)
		return
	}

	if s.proposalService == nil {
		http.Error(w, "proposal service not available", http.StatusServiceUnavailable)
		return
	}

	var params map[string]string
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&params)
	}

	if err := s.proposalService.Accept(id, params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status": "accepted",
		"id":     id,
	})
}

// handleIgnore 忽略提案
func (s *Server) handleIgnore(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id := r.URL.Path[len("/api/proposal/"):]
	id = id[:len(id)-len("/ignore")]

	if id == "" {
		http.Error(w, "proposal id required", http.StatusBadRequest)
		return
	}

	if s.proposalService == nil {
		http.Error(w, "proposal service not available", http.StatusServiceUnavailable)
		return
	}

	var params map[string]string
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&params)
	}

	if err := s.proposalService.Ignore(id, params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status": "ignored",
		"id":     id,
	})
}

// handleResubmit 重新分析
func (s *Server) handleResubmit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id := r.URL.Path[len("/api/proposal/"):]
	id = id[:len(id)-len("/resubmit")]

	if id == "" {
		http.Error(w, "proposal id required", http.StatusBadRequest)
		return
	}

	if s.proposalService == nil {
		http.Error(w, "proposal service not available", http.StatusServiceUnavailable)
		return
	}

	var params map[string]string
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&params)
	}

	proposal, err := s.proposalService.Resubmit(id, params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "resubmitted",
		"id":       id,
		"proposal": proposal,
	})
}

// handleIndex 处理前端页面
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write(indexHTML)
}

var indexHTML = []byte(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>安全运营龙虾</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
    <style>
        [x-cloak] { display: none !important; }
        .scrollbar-thin::-webkit-scrollbar { width: 6px; height: 6px; }
        .scrollbar-thin::-webkit-scrollbar-track { background: #1f2937; }
        .scrollbar-thin::-webkit-scrollbar-thumb { background: #4b5563; border-radius: 3px; }
        .scrollbar-thin::-webkit-scrollbar-thumb:hover { background: #6b7280; }
    </style>
</head>
<body class="bg-gray-900 text-gray-100" x-data="app()">
    <div class="h-screen flex flex-col">
        <!-- Header -->
        <header class="bg-gray-800 border-b border-gray-700 px-4 py-3 flex items-center justify-between">
            <div class="flex items-center space-x-3">
                <span class="text-2xl">🦞</span>
                <h1 class="text-xl font-bold">安全运营龙虾</h1>
            </div>
            <div class="flex items-center space-x-2">
                <template x-for="tab in tabs" :key="tab.id">
                    <button @click="activeTab = tab.id"
                            :class="activeTab === tab.id ? 'bg-blue-600 text-white' : 'bg-gray-700 text-gray-300 hover:bg-gray-600'"
                            class="px-4 py-2 rounded-lg font-medium transition-colors text-sm flex items-center space-x-2">
                        <span x-text="tab.icon"></span>
                        <span x-text="tab.name"></span>
                        <span x-show="tab.id === 'proposals' && pendingCount > 0"
                              x-text="pendingCount"
                              class="ml-1 bg-red-500 text-white text-xs px-2 py-0.5 rounded-full"></span>
                    </button>
                </template>
            </div>
        </header>

        <!-- Main Content -->
        <div class="flex-1 flex overflow-hidden">
            <!-- 对话 -->
            <div x-show="activeTab === 'chat'" x-cloak class="flex-1 flex flex-col">
                <!-- 消息列表 -->
                <div class="flex-1 overflow-y-auto p-4 space-y-4 scrollbar-thin">
                    <template x-for="(msg, idx) in messages" :key="idx">
                        <div :class="msg.role === 'user' ? 'ml-auto bg-blue-600' : 'mr-auto bg-gray-700'"
                             class="max-w-3xl rounded-lg p-3 px-4">
                            <div class="text-xs text-gray-400 mb-1" x-text="msg.role === 'user' ? '你' : '龙虾'"></div>
                            <div class="whitespace-pre-wrap" x-text="msg.content"></div>
                        </div>
                    </template>
                    <div x-show="messages.length === 0" class="text-center text-gray-500 py-8">
                        开始与安全运营龙虾对话吧
                    </div>
                </div>
                <!-- 输入框 -->
                <div class="p-4 border-t border-gray-700">
                    <form @submit.prevent="sendMessage" class="flex space-x-2">
                        <input type="text" x-model="inputMessage"
                               placeholder="输入消息..."
                               :disabled="isLoading"
                               class="flex-1 bg-gray-800 border border-gray-600 rounded-lg px-4 py-2 text-white placeholder-gray-400 focus:outline-none focus:border-blue-500">
                        <button type="submit"
                                :disabled="isLoading || !inputMessage.trim()"
                                class="px-6 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors">
                            <span x-show="!isLoading">发送</span>
                            <span x-show="isLoading">处理中...</span>
                        </button>
                    </form>
                </div>
            </div>

            <!-- 工具 -->
            <div x-show="activeTab === 'tools'" x-cloak class="flex-1 p-6 overflow-y-auto scrollbar-thin">
                <h2 class="text-xl font-bold mb-4">可用工具</h2>
                <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                    <template x-for="tool in tools" :key="tool">
                        <div class="bg-gray-800 rounded-lg p-4 border border-gray-700 hover:border-blue-500 transition-colors">
                            <div class="font-mono text-sm text-blue-400" x-text="tool"></div>
                        </div>
                    </template>
                </div>
                <div x-show="tools.length === 0" class="text-gray-500 text-center py-8">
                    暂无可用工具
                </div>
            </div>

            <!-- 技能 -->
            <div x-show="activeTab === 'skills'" x-cloak class="flex-1 p-6 overflow-y-auto scrollbar-thin">
                <h2 class="text-xl font-bold mb-4">已加载技能</h2>
                <div class="mb-4 text-sm text-gray-400">
                    共加载 <span x-text="skills.length" class="text-blue-400 font-bold"></span> 个技能
                </div>
                <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                    <template x-for="skill in skills" :key="skill.name">
                        <div class="bg-gray-800 rounded-lg p-4 border border-gray-700 hover:border-green-500 transition-colors">
                            <div class="flex items-center justify-between mb-2">
                                <div class="font-mono text-sm text-green-400" x-text="skill.name"></div>
                                <span class="text-xs px-2 py-1 rounded"
                                      :class="skill.source === 'workspace' ? 'bg-blue-900 text-blue-300' : (skill.source === 'global' ? 'bg-purple-900 text-purple-300' : 'bg-gray-700 text-gray-300')"
                                      x-text="skill.source"></span>
                            </div>
                            <div class="text-sm text-gray-400" x-text="skill.description || '无描述'"></div>
                        </div>
                    </template>
                </div>
                <div x-show="skills.length === 0" class="text-gray-500 text-center py-8">
                    暂无可用技能。请在 workspace/skills 目录下添加技能
                </div>
            </div>

            <!-- 提案 -->
            <div x-show="activeTab === 'proposals'" x-cloak class="flex-1 p-6 overflow-y-auto scrollbar-thin">
                <div class="flex items-center justify-between mb-4">
                    <h2 class="text-xl font-bold">安全运营提案</h2>
                    <button @click="fetchProposals()" class="text-gray-400 hover:text-white">
                        <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"></path></svg>
                    </button>
                </div>

                <!-- 待处理提案 -->
                <div x-show="pendingProposals.length > 0" class="mb-6">
                    <h3 class="text-sm font-medium text-gray-400 mb-3">待处理</h3>
                    <div class="grid gap-4 lg:grid-cols-2">
                        <template x-for="p in pendingProposals" :key="p.id">
                            <div class="bg-gray-800 rounded-lg p-4 border border-yellow-600 hover:border-yellow-500 transition-colors">
                                <div class="flex items-center justify-between mb-2">
                                    <span class="px-2 py-1 text-xs font-semibold rounded"
                                          :class="typeClass(p.type)" x-text="p.type"></span>
                                    <span class="text-xs text-gray-500" x-text="p.createdAt"></span>
                                </div>
                                <h4 class="font-bold mb-1" x-text="p.title"></h4>
                                <p class="text-sm text-gray-400 mb-3" x-text="p.summary"></p>
                                <div class="flex space-x-2">
                                    <button @click="acceptProposal(p.id)"
                                            class="px-3 py-1 bg-green-600 text-sm rounded hover:bg-green-700">确认</button>
                                    <button @click="ignoreProposal(p.id)"
                                            class="px-3 py-1 bg-gray-600 text-sm rounded hover:bg-gray-700">忽略</button>
                                    <button @click="viewProposal(p.id)"
                                            class="px-3 py-1 bg-blue-600 text-sm rounded hover:bg-blue-700">详情</button>
                                </div>
                            </div>
                        </template>
                    </div>
                </div>

                <!-- 所有提案 -->
                <div>
                    <h3 class="text-sm font-medium text-gray-400 mb-3">全部提案</h3>
                    <div class="bg-gray-800 rounded-lg overflow-hidden">
                        <table class="min-w-full">
                            <thead class="bg-gray-700">
                                <tr>
                                    <th class="px-4 py-2 text-left text-xs font-medium text-gray-300">类型</th>
                                    <th class="px-4 py-2 text-left text-xs font-medium text-gray-300">标题</th>
                                    <th class="px-4 py-2 text-left text-xs font-medium text-gray-300">状态</th>
                                    <th class="px-4 py-2 text-left text-xs font-medium text-gray-300">创建时间</th>
                                    <th class="px-4 py-2 text-left text-xs font-medium text-gray-300">操作</th>
                                </tr>
                            </thead>
                            <tbody class="divide-y divide-gray-700">
                                <template x-for="p in proposals" :key="p.id">
                                    <tr class="hover:bg-gray-750">
                                        <td class="px-4 py-2">
                                            <span class="px-2 py-1 text-xs font-semibold rounded"
                                                  :class="typeClass(p.type)" x-text="p.type"></span>
                                        </td>
                                        <td class="px-4 py-2">
                                            <div class="text-sm font-medium" x-text="p.title"></div>
                                            <div class="text-xs text-gray-500" x-text="p.summary"></div>
                                        </td>
                                        <td class="px-4 py-2">
                                            <span class="px-2 py-1 text-xs font-semibold rounded"
                                                  :class="statusClass(p.status)" x-text="statusText(p.status)"></span>
                                        </td>
                                        <td class="px-4 py-2 text-sm text-gray-400" x-text="p.createdAt"></td>
                                        <td class="px-4 py-2">
                                            <button @click="viewProposal(p.id)"
                                                    class="text-blue-400 hover:text-blue-300 text-sm">查看</button>
                                        </td>
                                    </tr>
                                </template>
                                <tr x-show="proposals.length === 0">
                                    <td colspan="5" class="px-4 py-8 text-center text-gray-500">
                                        暂无提案
                                    </td>
                                </tr>
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>

            <!-- 设置 -->
            <div x-show="activeTab === 'settings'" x-cloak class="flex-1 p-6 overflow-y-auto scrollbar-thin">
                <h2 class="text-xl font-bold mb-4">系统信息</h2>
                <div class="bg-gray-800 rounded-lg p-4 border border-gray-700">
                    <pre class="text-sm text-gray-300 whitespace-pre-wrap" x-text="JSON.stringify(info, null, 2)"></pre>
                </div>
            </div>
        </div>

        <!-- Proposal Modal -->
        <div x-show="showModal" x-cloak class="fixed inset-0 z-50 overflow-y-auto">
            <div class="flex items-center justify-center min-h-screen p-4">
                <div x-show="showModal"
                     x-transition:enter="ease-out duration-200"
                     x-transition:enter-start="opacity-0"
                     x-transition:enter-end="opacity-100"
                     x-transition:leave="ease-in duration-150"
                     x-transition:leave-start="opacity-100"
                     x-transition:leave-end="opacity-0"
                     class="fixed inset-0 bg-black bg-opacity-75"
                     @click="showModal = false"></div>

                <div x-show="showModal"
                     x-transition:enter="ease-out duration-200"
                     x-transition:enter-start="opacity-0 scale-95"
                     x-transition:enter-end="opacity-100 scale-100"
                     x-transition:leave="ease-in duration-150"
                     x-transition:leave-start="opacity-100 scale-100"
                     x-transition:leave-end="opacity-0 scale-95"
                     class="relative bg-gray-800 rounded-xl shadow-2xl w-full max-w-2xl">

                    <template x-if="currentProposal">
                        <div>
                            <div class="p-6">
                                <div class="flex items-center justify-between mb-4">
                                    <span class="px-3 py-1 text-sm font-semibold rounded"
                                          :class="typeClass(currentProposal.type)" x-text="currentProposal.type"></span>
                                    <span class="text-sm text-gray-400" x-text="currentProposal.createdAt"></span>
                                </div>
                                <h3 class="text-xl font-bold mb-2" x-text="currentProposal.title"></h3>
                                <p class="text-gray-400 mb-4" x-text="currentProposal.summary"></p>

                                <div class="bg-gray-900 rounded-lg p-4 mb-4">
                                    <h4 class="text-sm font-medium text-gray-400 mb-2">详细信息</h4>
                                    <div class="grid grid-cols-2 gap-2 text-sm">
                                        <template x-for="(value, key) in currentProposal.details" :key="key">
                                            <div class="col-span-2 sm:col-span-1">
                                                <span class="text-gray-500" x-text="key + ':'"></span>
                                                <span class="text-gray-300 ml-1" x-text="value"></span>
                                            </div>
                                        </template>
                                    </div>
                                </div>

                                <div x-show="Object.keys(currentProposal.parameters || {}).length > 0">
                                    <h4 class="text-sm font-medium text-gray-400 mb-2">可调整参数</h4>
                                    <div class="space-y-3 mb-4">
                                        <template x-for="(param, key) in currentProposal.parameters" :key="key">
                                            <div>
                                                <label class="block text-sm font-medium text-gray-300 mb-1" x-text="param.label"></label>
                                                <input type="text" x-model="param.value"
                                                       class="w-full bg-gray-900 border border-gray-600 rounded px-3 py-2 text-white focus:outline-none focus:border-blue-500">
                                            </div>
                                        </template>
                                    </div>
                                </div>
                            </div>
                            <div class="px-6 py-4 bg-gray-750 rounded-b-xl flex justify-end space-x-3">
                                <button @click="showModal = false"
                                        class="px-4 py-2 bg-gray-700 text-white rounded-lg hover:bg-gray-600">关闭</button>
                                                <template x-if="currentProposal.status === 'pending'">
                                                    <div class="flex space-x-2">
                                                        <button @click="ignoreProposal(currentProposal.id); showModal = false"
                                                                class="px-4 py-2 bg-gray-600 text-white rounded-lg hover:bg-gray-500">忽略</button>
                                                        <button @click="acceptProposal(currentProposal.id); showModal = false"
                                                                class="px-4 py-2 bg-green-600 text-white rounded-lg hover:bg-green-500">确认</button>
                                    </div>
                                </template>
                            </div>
                        </div>
                    </template>
                </div>
            </div>
        </div>
    </div>

    <script>
        function app() {
            return {
                activeTab: 'chat',
                tabs: [
                    { id: 'chat', name: '对话', icon: '💬' },
                    { id: 'tools', name: '工具', icon: '🔧' },
                    { id: 'skills', name: '技能', icon: '✨' },
                    { id: 'proposals', name: '提案', icon: '📋' },
                    { id: 'settings', name: '设置', icon: '⚙️' }
                ],
                messages: [],
                inputMessage: '',
                isLoading: false,
                tools: [],
                skills: [],
                proposals: [],
                currentProposal: null,
                showModal: false,
                info: {},

                init() {
                    this.fetchInfo();
                    this.fetchTools();
                    this.fetchSkills();
                    this.fetchProposals();
                    setInterval(() => this.fetchProposals(), 5000);
                },

                async fetchInfo() {
                    try {
                        const response = await fetch('/api/info');
                        this.info = await response.json();
                    } catch (e) {
                        console.error('Failed to fetch info:', e);
                    }
                },

                async fetchTools() {
                    try {
                        const response = await fetch('/api/tools');
                        const data = await response.json();
                        this.tools = data.tools || [];
                    } catch (e) {
                        console.error('Failed to fetch tools:', e);
                    }
                },

                async fetchSkills() {
                    try {
                        const response = await fetch('/api/skills');
                        const data = await response.json();
                        this.skills = data.skills || [];
                    } catch (e) {
                        console.error('Failed to fetch skills:', e);
                    }
                },

                async fetchProposals() {
                    try {
                        const response = await fetch('/api/proposals');
                        this.proposals = await response.json();
                    } catch (e) {
                        console.error('Failed to fetch proposals:', e);
                    }
                },

                get pendingProposals() {
                    return this.proposals.filter(p => p.status === 'pending');
                },

                get pendingCount() {
                    return this.pendingProposals.length;
                },

                async sendMessage() {
                    if (!this.inputMessage.trim() || this.isLoading) return;

                    const message = this.inputMessage.trim();
                    this.inputMessage = '';
                    this.isLoading = true;

                    this.messages.push({ role: 'user', content: message });

                    try {
                        const response = await fetch('/api/chat', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ message: message })
                        });
                        const data = await response.json();
                        this.messages.push({ role: 'assistant', content: data.response || data.error || '无响应' });
                    } catch (e) {
                        this.messages.push({ role: 'assistant', content: '错误: ' + e.message });
                    } finally {
                        this.isLoading = false;
                    }
                },

                async viewProposal(id) {
                    try {
                        const response = await fetch('/api/proposal/' + id);
                        this.currentProposal = await response.json();
                        this.showModal = true;
                    } catch (e) {
                        console.error('Failed to fetch proposal:', e);
                    }
                },

                async acceptProposal(id) {
                    try {
                        await fetch('/api/proposal/' + id + '/accept', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({})
                        });
                        this.fetchProposals();
                    } catch (e) {
                        console.error('Failed to accept proposal:', e);
                    }
                },

                async ignoreProposal(id) {
                    try {
                        await fetch('/api/proposal/' + id + '/ignore', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({})
                        });
                        this.fetchProposals();
                    } catch (e) {
                        console.error('Failed to ignore proposal:', e);
                    }
                },

                typeClass(type) {
                    const classes = {
                        'risk': 'bg-red-900 text-red-300',
                        'weak': 'bg-yellow-900 text-yellow-300',
                        'api_biz': 'bg-blue-900 text-blue-300',
                        'app': 'bg-purple-900 text-purple-300'
                    };
                    return classes[type] || 'bg-gray-700 text-gray-300';
                },

                statusClass(status) {
                    const classes = {
                        'pending': 'bg-yellow-900 text-yellow-300',
                        'accepted': 'bg-green-900 text-green-300',
                        'ignored': 'bg-gray-700 text-gray-300',
                        'modified': 'bg-blue-900 text-blue-300'
                    };
                    return classes[status] || 'bg-gray-700 text-gray-300';
                },

                statusText(status) {
                    const texts = {
                        'pending': '待处理',
                        'accepted': '已确认',
                        'ignored': '已忽略',
                        'modified': '已修改'
                    };
                    return texts[status] || status;
                }
            }
        }
    </script>
</body>
</html>
`)
