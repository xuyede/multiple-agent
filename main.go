package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"muitl-agent/internal/agent"
	"muitl-agent/internal/llm"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// Task 子任务
type Task struct {
	ID          string
	Description string
	AgentType   string
	Status      string
	Result      string
}

// Orchestrator 多 Agent 编排器
// 职责：分解任务 → 分派给 Agent → 汇总结果
type Orchestrator struct {
	claude    *llm.ClaudeClient
	mcpClient *client.Client
	agents    map[string]*agent.Agent
	tasks     []Task
	mu        sync.Mutex
}

func main() {
	ctx := context.Background()

	// 1. 连接 MCP Server（需要先启动 cmd/server）
	mcpClient, err := client.NewSSEMCPClient("http://localhost:8080/sse")
	if err != nil {
		log.Fatalf("Failed to create MCP client: %v", err)
	}

	if err := mcpClient.Start(ctx); err != nil {
		log.Fatalf("Failed to connect to MCP Server: %v", err)
	}
	defer mcpClient.Close()

	// 初始化 MCP 连接
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "Go Orchestrator",
		Version: "1.0.0",
	}
	_, err = mcpClient.Initialize(ctx, initReq)
	if err != nil {
		log.Fatalf("Failed to initialize MCP: %v", err)
	}

	log.Println("✅ Connected to MCP Server")

	// 2. 创建 Claude 客户端
	claude := llm.NewClaudeClient("claude-sonnet-4-20250514")

	// 3. 创建 Orchestrator 并注册 Agents
	o := &Orchestrator{
		claude:    claude,
		mcpClient: mcpClient,
		agents: map[string]*agent.Agent{
			"code":   agent.NewCodeAgent(claude, mcpClient),
			"test":   agent.NewTestAgent(claude, mcpClient),
			"review": agent.NewReviewAgent(claude, mcpClient),
		},
	}

	// 4. 处理用户请求
	err = o.ProcessRequest(ctx, "帮我实现一个 Go 的并发安全的缓存，支持 TTL 过期")
	if err != nil {
		log.Fatal(err)
	}

	// 5. 输出所有任务结果
	fmt.Println("\n========== 任务执行结果 ==========")
	for _, task := range o.tasks {
		fmt.Printf("\n[%s] %s -> %s\n", task.AgentType, task.Description, task.Status)
		if task.Result != "" {
			fmt.Printf("  Result: %.500s\n", task.Result)
		}
	}
}

// ProcessRequest 处理用户请求：分解任务 → 分派给 Agent → 汇总结果
func (o *Orchestrator) ProcessRequest(ctx context.Context, userRequest string) error {
	// 1. 让 Claude 分解任务
	messages := []llm.Message{
		{Role: "user", Content: fmt.Sprintf(`你是一个 Go 开发团队的技术负责人。请将以下需求分解为可执行的子任务，使用 assign_task 工具分配给对应的 Agent。

可用 Agent 及其能力：
- code: 代码执行 Agent，能编写并运行 Go 代码，验证语法和逻辑正确性
- test: 测试 Agent，能运行 Go 单元测试并分析结果
- review: 审查 Agent，能使用 golangci-lint 审查代码质量

每个 Agent 是独立的，会自主完成分配给它的任务。请为每个子任务提供清晰的描述。

需求：%s`, userRequest)},
	}

	tools := []llm.Tool{
		{
			Name:        "assign_task",
			Description: "分配子任务给特定 Agent",
			InputSchema: llm.InputSchema{
				Type:     "object",
				Required: []string{"agent", "description"},
				Properties: map[string]interface{}{
					"agent": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"code", "test", "review"},
						"description": "目标 Agent 类型",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "任务的详细描述，Agent 会据此自主完成任务",
					},
				},
			},
		},
	}

	resp, err := o.claude.Chat(ctx, messages, tools)
	if err != nil {
		return fmt.Errorf("claude chat failed: %w", err)
	}

	// 2. 解析任务分配
	for _, content := range resp.Content {
		if content.Type == "tool_use" && content.Name == "assign_task" {
			desc, _ := content.Input["description"].(string)
			agentType, _ := content.Input["agent"].(string)
			if desc == "" || agentType == "" {
				continue
			}
			task := Task{
				ID:          fmt.Sprintf("task-%d", len(o.tasks)+1),
				Description: desc,
				AgentType:   agentType,
				Status:      "pending",
			}
			o.tasks = append(o.tasks, task)
		}
	}

	if len(o.tasks) == 0 {
		log.Println("No tasks assigned by Claude")
		return nil
	}

	log.Printf("📋 Orchestrator 分解出 %d 个子任务，分派给 Agent 执行", len(o.tasks))

	// 3. 并行将子任务分派给对应的 Agent
	var wg sync.WaitGroup
	for i := range o.tasks {
		wg.Add(1)
		go func(task *Task) {
			defer wg.Done()
			o.dispatchToAgent(ctx, task)
		}(&o.tasks[i])
	}
	wg.Wait()

	// 4. 汇总结果
	log.Printf("✅ All %d tasks completed", len(o.tasks))
	return nil
}

// dispatchToAgent 将任务分派给对应的 Agent 执行
func (o *Orchestrator) dispatchToAgent(ctx context.Context, task *Task) {
	// 查找对应的 Agent
	ag, ok := o.agents[task.AgentType]
	if !ok {
		o.mu.Lock()
		task.Status = "failed"
		task.Result = fmt.Sprintf("unknown agent type: %s", task.AgentType)
		o.mu.Unlock()
		return
	}

	log.Printf("📤 Dispatching to %s: %s", ag.Name, task.Description)

	// Agent 自主执行任务（agentic loop）
	result := ag.Run(ctx, task.Description)

	// 写回结果
	o.mu.Lock()
	defer o.mu.Unlock()

	if result.Success {
		task.Status = "completed"
	} else {
		task.Status = "failed"
	}
	task.Result = result.Output
}
