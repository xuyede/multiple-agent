package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"muitl-agent/internal/llm"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// Task 子任务
type Task struct {
	ID          string
	Description string
	Agent       string
	Status      string
	Result      string
}

// Orchestrator 多 Agent 编排器
type Orchestrator struct {
	claude    *llm.ClaudeClient
	mcpClient *client.Client
	tasks     []Task
	mu        sync.Mutex
}

// agentToolMap 将 agent 类型映射到 MCP 工具名
var agentToolMap = map[string]string{
	"code":   "execute_code",
	"test":   "run_tests",
	"review": "code_review",
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

	// 2. 创建 Orchestrator
	o := &Orchestrator{
		claude:    llm.NewClaudeClient("claude-sonnet-4-20250514"),
		mcpClient: mcpClient,
	}

	// 3. 处理用户请求
	err = o.ProcessRequest(ctx, "帮我实现一个 Go 的并发安全的缓存，支持 TTL 过期")
	if err != nil {
		log.Fatal(err)
	}

	// 4. 输出所有任务结果
	fmt.Println("\n========== 任务执行结果 ==========")
	for _, task := range o.tasks {
		fmt.Printf("\n[%s] %s -> %s\n", task.Agent, task.Description, task.Status)
		if task.Result != "" {
			fmt.Printf("  Result: %.500s\n", task.Result) // 截断过长输出
		}
	}
}

// ProcessRequest 处理用户请求，分解为子任务并行执行
func (o *Orchestrator) ProcessRequest(ctx context.Context, userRequest string) error {
	// 1. 让 Claude 分解任务
	messages := []llm.Message{
		{Role: "user", Content: fmt.Sprintf(`你是一个 Go 开发团队的技术负责人。请将以下需求分解为可执行的子任务，使用 assign_task 工具分配给对应的 Agent。

可用 Agent 及其能力：
- code: 执行 Go 代码，验证语法和逻辑正确性
- test: 运行 Go 单元测试
- review: 使用 golangci-lint 审查代码质量

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
						"description": "任务描述",
					},
					"code": map[string]interface{}{
						"type":        "string",
						"description": "需要执行/测试/审查的 Go 代码（可选）",
					},
					"package": map[string]interface{}{
						"type":        "string",
						"description": "需要测试的包路径（仅 test agent 使用，可选）",
					},
				},
			},
		},
	}

	resp, err := o.claude.Chat(ctx, messages, tools)
	if err != nil {
		return fmt.Errorf("claude chat failed: %w", err)
	}

	// 2. 解析任务分配（匹配 Anthropic tool_use 格式）
	for _, content := range resp.Content {
		if content.Type == "tool_use" && content.Name == "assign_task" {
			desc, _ := content.Input["description"].(string)
			agentName, _ := content.Input["agent"].(string)
			if desc == "" || agentName == "" {
				continue
			}
			task := Task{
				ID:          fmt.Sprintf("task-%d", len(o.tasks)+1),
				Description: desc,
				Agent:       agentName,
				Status:      "pending",
			}
			// 保存 Claude 提供的额外参数（code、package 等）
			if code, ok := content.Input["code"].(string); ok {
				task.Result = code // 临时存放，executeTask 时使用
			}
			o.tasks = append(o.tasks, task)
		}
	}

	if len(o.tasks) == 0 {
		log.Println("No tasks assigned by Claude")
		return nil
	}

	log.Printf("📋 Claude 分解出 %d 个子任务", len(o.tasks))

	// 3. 并行执行子任务（通过 MCP Server）
	var wg sync.WaitGroup
	for i := range o.tasks {
		wg.Add(1)
		go func(task *Task) {
			defer wg.Done()
			o.executeTask(ctx, task)
		}(&o.tasks[i])
	}
	wg.Wait()

	// 4. 汇总结果
	log.Printf("✅ All %d tasks completed", len(o.tasks))
	return nil
}

// executeTask 通过 MCP Server 执行单个子任务
func (o *Orchestrator) executeTask(ctx context.Context, task *Task) {
	log.Printf("🔄 Executing task [%s]: %s", task.Agent, task.Description)

	// 确定要调用的 MCP 工具
	toolName, ok := agentToolMap[task.Agent]
	if !ok {
		o.mu.Lock()
		task.Status = "failed"
		task.Result = fmt.Sprintf("unknown agent type: %s", task.Agent)
		o.mu.Unlock()
		return
	}

	// 构建工具调用参数
	args := o.buildToolArgs(ctx, task, toolName)
	if args == nil {
		o.mu.Lock()
		task.Status = "failed"
		task.Result = "failed to build tool arguments"
		o.mu.Unlock()
		return
	}

	// 通过 MCP Client 调用 MCP Server 的工具
	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = toolName
	callReq.Params.Arguments = args
	result, err := o.mcpClient.CallTool(ctx, callReq)

	o.mu.Lock()
	defer o.mu.Unlock()

	if err != nil {
		task.Status = "failed"
		task.Result = fmt.Sprintf("MCP call failed: %v", err)
		return
	}

	// 解析 MCP 工具返回的结果
	task.Status = "completed"
	if result.IsError {
		task.Status = "failed"
	}
	if len(result.Content) > 0 {
		// 收集所有文本内容
		var texts []string
		for _, c := range result.Content {
			if textContent, ok := c.(mcp.TextContent); ok {
				texts = append(texts, textContent.Text)
			}
		}
		if len(texts) > 0 {
			task.Result = texts[0]
		}
	}
}

// buildToolArgs 根据任务类型构建 MCP 工具参数
// 如果 Claude 已经提供了代码，直接使用；否则让 Claude 生成代码
func (o *Orchestrator) buildToolArgs(ctx context.Context, task *Task, toolName string) map[string]interface{} {
	switch toolName {
	case "execute_code", "code_review":
		code := task.Result // Claude 在分解阶段可能已提供代码
		task.Result = ""    // 清除临时数据

		if code == "" {
			// 让 Claude 为这个任务生成代码
			code = o.generateCode(ctx, task.Description)
		}
		if code == "" {
			return nil
		}
		return map[string]interface{}{"code": code}

	case "run_tests":
		return map[string]interface{}{"package": "./..."}

	default:
		return nil
	}
}

// generateCode 让 Claude 为指定任务生成 Go 代码
func (o *Orchestrator) generateCode(ctx context.Context, taskDescription string) string {
	messages := []llm.Message{
		{Role: "user", Content: fmt.Sprintf(`请为以下任务生成完整的、可直接运行的 Go 代码（包含 package main 和 func main）。
只输出代码，不要任何解释。

任务：%s`, taskDescription)},
	}

	resp, err := o.claude.Chat(ctx, messages, nil)
	if err != nil {
		log.Printf("❌ Generate code failed: %v", err)
		return ""
	}

	for _, content := range resp.Content {
		if content.Type == "text" && content.Text != "" {
			return extractCode(content.Text)
		}
	}
	return ""
}

// extractCode 从 Claude 的响应中提取代码块
func extractCode(text string) string {
	// 尝试提取 ```go ... ``` 代码块
	const startMarker = "```go\n"
	const endMarker = "\n```"

	start := 0
	for i := 0; i < len(text)-len(startMarker); i++ {
		if text[i:i+len(startMarker)] == startMarker {
			start = i + len(startMarker)
			break
		}
	}

	if start > 0 {
		for i := start; i < len(text)-len(endMarker)+1; i++ {
			if text[i:i+len(endMarker)] == endMarker {
				return text[start:i]
			}
		}
	}

	// 没有代码块标记，直接返回原文
	return text
}
