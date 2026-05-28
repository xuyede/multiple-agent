package agent

import (
	"context"
	"fmt"
	"log"

	"muitl-agent/internal/llm"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

const maxAgentIterations = 5

// Agent 独立的 AI Agent，拥有自主决策能力
// 每个 Agent 有自己的系统提示词、可用工具列表，能与 LLM 进行多轮对话并调用 MCP 工具
type Agent struct {
	Name         string
	SystemPrompt string
	Tools        []llm.Tool // Agent 可用的工具定义（告知 LLM）
	MCPTools     []string   // Agent 可调用的 MCP 工具名列表

	claude    *llm.ClaudeClient
	mcpClient *client.Client
}

// AgentResult Agent 执行结果
type AgentResult struct {
	Success bool
	Output  string
}

// NewCodeAgent 创建代码执行 Agent
func NewCodeAgent(claude *llm.ClaudeClient, mcpClient *client.Client) *Agent {
	return &Agent{
		Name: "CodeAgent",
		SystemPrompt: `你是一个专业的 Go 代码执行 Agent。你的职责是：
1. 根据任务描述编写可运行的 Go 代码
2. 使用 execute_code 工具执行代码
3. 分析执行结果，如果有错误则修复代码并重新执行
4. 直到代码正确运行，返回最终结果

注意：代码必须是完整的、可独立运行的（包含 package main 和 func main）。`,
		Tools: []llm.Tool{
			{
				Name:        "execute_code",
				Description: "执行 Go 代码并返回结果",
				InputSchema: llm.InputSchema{
					Type:     "object",
					Required: []string{"code"},
					Properties: map[string]interface{}{
						"code": map[string]interface{}{
							"type":        "string",
							"description": "要执行的完整 Go 代码",
						},
					},
				},
			},
		},
		MCPTools:  []string{"execute_code"},
		claude:    claude,
		mcpClient: mcpClient,
	}
}

// NewTestAgent 创建测试执行 Agent
func NewTestAgent(claude *llm.ClaudeClient, mcpClient *client.Client) *Agent {
	return &Agent{
		Name: "TestAgent",
		SystemPrompt: `你是一个专业的 Go 测试 Agent。你的职责是：
1. 根据任务描述理解需要测试的功能
2. 使用 run_tests 工具运行测试
3. 分析测试结果，报告通过/失败情况
4. 如果测试失败，分析失败原因并给出修复建议`,
		Tools: []llm.Tool{
			{
				Name:        "run_tests",
				Description: "运行 Go 单元测试",
				InputSchema: llm.InputSchema{
					Type:     "object",
					Required: []string{"package"},
					Properties: map[string]interface{}{
						"package": map[string]interface{}{
							"type":        "string",
							"description": "要测试的包路径，例如 ./... 或 ./pkg/cache",
						},
					},
				},
			},
		},
		MCPTools:  []string{"run_tests"},
		claude:    claude,
		mcpClient: mcpClient,
	}
}

// NewReviewAgent 创建代码审查 Agent
func NewReviewAgent(claude *llm.ClaudeClient, mcpClient *client.Client) *Agent {
	return &Agent{
		Name: "ReviewAgent",
		SystemPrompt: `你是一个专业的 Go 代码审查 Agent。你的职责是：
1. 根据任务描述获取需要审查的代码
2. 使用 code_review 工具进行静态分析
3. 综合分析审查结果，给出代码质量评估
4. 提供具体的改进建议`,
		Tools: []llm.Tool{
			{
				Name:        "code_review",
				Description: "使用 golangci-lint 审查 Go 代码质量",
				InputSchema: llm.InputSchema{
					Type:     "object",
					Required: []string{"code"},
					Properties: map[string]interface{}{
						"code": map[string]interface{}{
							"type":        "string",
							"description": "要审查的 Go 代码",
						},
					},
				},
			},
		},
		MCPTools:  []string{"code_review"},
		claude:    claude,
		mcpClient: mcpClient,
	}
}

// Run 执行 Agent 的 agentic loop
// Agent 自主与 LLM 对话，决定调用哪些工具，直到任务完成
func (a *Agent) Run(ctx context.Context, taskDescription string) AgentResult {
	log.Printf("🤖 [%s] Starting task: %s", a.Name, taskDescription)

	// 初始化对话历史
	messages := []llm.Message{
		{Role: "user", Content: fmt.Sprintf("%s\n\n请执行以下任务：\n%s", a.SystemPrompt, taskDescription)},
	}

	// Agentic Loop：多轮对话直到 Agent 完成任务或达到最大迭代
	for i := 0; i < maxAgentIterations; i++ {
		log.Printf("🔁 [%s] Iteration %d/%d", a.Name, i+1, maxAgentIterations)

		// 调用 LLM
		resp, err := a.claude.Chat(ctx, messages, a.Tools)
		if err != nil {
			return AgentResult{Success: false, Output: fmt.Sprintf("LLM call failed: %v", err)}
		}

		// 收集本轮响应
		hasToolUse := false
		var textOutput string

		// 构建 assistant 消息（包含所有 content blocks）
		// 简化处理：将 assistant 的文本回复和 tool_use 都记录下来
		assistantText := ""
		var toolCalls []llm.ContentBlock

		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				assistantText += block.Text
				textOutput = block.Text
			case "tool_use":
				hasToolUse = true
				toolCalls = append(toolCalls, block)
			}
		}

		// 如果没有工具调用，Agent 认为任务完成
		if !hasToolUse {
			log.Printf("✅ [%s] Task completed (no more tool calls)", a.Name)
			return AgentResult{Success: true, Output: textOutput}
		}

		// 将 assistant 的回复加入历史
		messages = append(messages, llm.Message{
			Role:    "assistant",
			Content: assistantText,
		})

		// 执行所有工具调用，收集结果
		var toolResults []string
		for _, tc := range toolCalls {
			log.Printf("🔧 [%s] Calling tool: %s", a.Name, tc.Name)

			// 验证 Agent 是否有权调用此工具
			if !a.canUseTool(tc.Name) {
				toolResults = append(toolResults, fmt.Sprintf("[%s] Error: tool not available for this agent", tc.Name))
				continue
			}

			// 通过 MCP Client 调用工具
			result := a.callMCPTool(ctx, tc.Name, tc.Input)
			toolResults = append(toolResults, fmt.Sprintf("[%s] %s", tc.Name, result))
			log.Printf("📋 [%s] Tool %s result: %.200s", a.Name, tc.Name, result)
		}

		// 将工具结果作为 user 消息反馈给 LLM
		toolResultMsg := "工具执行结果：\n"
		for _, r := range toolResults {
			toolResultMsg += r + "\n"
		}
		messages = append(messages, llm.Message{
			Role:    "user",
			Content: toolResultMsg,
		})
	}

	return AgentResult{
		Success: false,
		Output:  fmt.Sprintf("[%s] reached max iterations (%d) without completing", a.Name, maxAgentIterations),
	}
}

// canUseTool 检查 Agent 是否有权使用某个工具
func (a *Agent) canUseTool(toolName string) bool {
	for _, t := range a.MCPTools {
		if t == toolName {
			return true
		}
	}
	return false
}

// callMCPTool 通过 MCP Client 调用 MCP Server 上的工具
func (a *Agent) callMCPTool(ctx context.Context, toolName string, args map[string]interface{}) string {
	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = toolName
	callReq.Params.Arguments = args

	result, err := a.mcpClient.CallTool(ctx, callReq)
	if err != nil {
		return fmt.Sprintf("Error: MCP call failed: %v", err)
	}

	if result.IsError {
		// 提取错误文本
		for _, c := range result.Content {
			if textContent, ok := c.(mcp.TextContent); ok {
				return fmt.Sprintf("Error: %s", textContent.Text)
			}
		}
		return "Error: unknown tool error"
	}

	// 提取成功结果
	var texts []string
	for _, c := range result.Content {
		if textContent, ok := c.(mcp.TextContent); ok {
			texts = append(texts, textContent.Text)
		}
	}
	if len(texts) > 0 {
		return texts[0]
	}
	return "(empty result)"
}
