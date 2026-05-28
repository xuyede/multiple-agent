package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	// maxCodeSize 代码最大长度限制（64KB）
	maxCodeSize = 64 * 1024
	// executeTimeout 代码执行超时时间
	executeTimeout = 30 * time.Second
)

// HandleExecuteCodeMCP 适配 mcp-go 的工具处理器签名
func HandleExecuteCodeMCP(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.Params.Arguments
	return HandleExecuteCode(ctx, args)
}

// HandleRunTestsMCP 适配 mcp-go 的工具处理器签名
func HandleRunTestsMCP(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.Params.Arguments
	return HandleRunTests(ctx, args)
}

// HandleCodeReviewMCP 适配 mcp-go 的工具处理器签名
func HandleCodeReviewMCP(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.Params.Arguments
	return HandleCodeReview(ctx, args)
}

// HandleExecuteCode 执行 Go 代码并返回结果
func HandleExecuteCode(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	code, ok := args["code"].(string)
	if !ok {
		return mcp.NewToolResultError("code parameter required"), nil
	}

	// 安全检查：限制代码大小
	if len(code) > maxCodeSize {
		return mcp.NewToolResultError(fmt.Sprintf("code too large: %d bytes (max %d)", len(code), maxCodeSize)), nil
	}

	// 安全检查：禁止危险操作
	if containsDangerousCode(code) {
		return mcp.NewToolResultError("code contains potentially dangerous operations"), nil
	}

	// 创建临时目录
	tmpDir, err := os.MkdirTemp("", "go-agent-*")
	if err != nil {
		return mcp.NewToolResultError("create temp dir failed: " + err.Error()), nil
	}
	defer os.RemoveAll(tmpDir)

	// 写入代码文件
	mainFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainFile, []byte(code), 0644); err != nil {
		return mcp.NewToolResultError("write file failed: " + err.Error()), nil
	}

	// 执行代码，使用独立的 GOCACHE 避免并发冲突，并设置超时
	execCtx, cancel := context.WithTimeout(ctx, executeTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "go", "run", mainFile)
	cmd.Env = append(os.Environ(),
		"GOCACHE="+filepath.Join(tmpDir, "go-cache"),
		"GOMODCACHE="+filepath.Join(tmpDir, "go-mod-cache"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return mcp.NewToolResultError("execution timed out (30s limit)"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("execution failed: %s\n%s", err, output)), nil
	}

	return mcp.NewToolResultText(string(output)), nil
}

// containsDangerousCode 检查代码中是否包含危险操作
func containsDangerousCode(code string) bool {
	dangerous := []string{
		"os.RemoveAll",
		"os.Remove(",
		"syscall.Exec",
		"os/exec",
		"net/http",
		"\"unsafe\"",
	}
	for _, pattern := range dangerous {
		if strings.Contains(code, pattern) {
			return true
		}
	}
	return false
}

// HandleRunTests 运行 Go 单元测试
func HandleRunTests(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	pkg, ok := args["package"].(string)
	if !ok {
		return mcp.NewToolResultError("package parameter required"), nil
	}

	// 验证包路径格式，防止命令注入
	validPkg := regexp.MustCompile(`^[a-zA-Z0-9_\-./]+$`)
	if !validPkg.MatchString(pkg) {
		return mcp.NewToolResultError("invalid package path format"), nil
	}

	// 运行测试，设置超时
	execCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "go", "test", "-v", "-count=1", pkg)
	output, err := cmd.CombinedOutput()

	result := string(output)
	if execCtx.Err() == context.DeadlineExceeded {
		return mcp.NewToolResultError("test execution timed out (60s limit)"), nil
	}
	if err != nil {
		result = fmt.Sprintf("Tests failed:\n%s", result)
	} else {
		result = fmt.Sprintf("Tests passed:\n%s", result)
	}

	return mcp.NewToolResultText(result), nil
}

// HandleCodeReview 使用 golangci-lint 审查代码质量
func HandleCodeReview(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	code, ok := args["code"].(string)
	if !ok {
		return mcp.NewToolResultError("code parameter required"), nil
	}

	// 创建临时目录
	tmpDir, err := os.MkdirTemp("", "go-review-*")
	if err != nil {
		return mcp.NewToolResultError("create temp dir failed: " + err.Error()), nil
	}
	defer os.RemoveAll(tmpDir)

	// 写入代码文件
	mainFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainFile, []byte(code), 0644); err != nil {
		return mcp.NewToolResultError("write file failed: " + err.Error()), nil
	}

	// 使用 golangci-lint 进行代码审查
	cmd := exec.CommandContext(ctx, "golangci-lint", "run", "--disable-all",
		"-E", "govet", "-E", "staticcheck", tmpDir)
	output, _ := cmd.CombinedOutput()

	issues := strings.TrimSpace(string(output))
	if issues == "" {
		return mcp.NewToolResultText("✅ Code looks good! No issues found."), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("⚠️ Found issues:\n\n%s", issues)), nil
}
