package agent

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterTools 注册所有工具到 MCP Server
func RegisterTools(s *server.MCPServer) {
	// 执行 Go 代码工具
	executeTool := mcp.NewTool("execute_code",
		mcp.WithDescription("执行 Go 代码并返回结果"),
		mcp.WithString("code",
			mcp.Required(),
			mcp.Description("要执行的 Go 代码"),
		),
	)
	s.AddTool(executeTool, HandleExecuteCodeMCP)

	// 运行测试工具
	testTool := mcp.NewTool("run_tests",
		mcp.WithDescription("运行 Go 单元测试"),
		mcp.WithString("package",
			mcp.Required(),
			mcp.Description("要测试的包路径"),
		),
	)
	s.AddTool(testTool, HandleRunTestsMCP)

	// 代码审查工具
	reviewTool := mcp.NewTool("code_review",
		mcp.WithDescription("审查 Go 代码质量"),
		mcp.WithString("code",
			mcp.Required(),
			mcp.Description("要审查的代码"),
		),
	)
	s.AddTool(reviewTool, HandleCodeReviewMCP)
}
