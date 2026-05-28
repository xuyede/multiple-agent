package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ClaudeClient Claude API 客户端
type ClaudeClient struct {
	apiKey     string
	model      string
	maxTokens  int
	httpClient *http.Client
}

// Message 消息结构
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature,omitempty"`
	Tools       []Tool    `json:"tools,omitempty"`
}

// Tool 工具定义
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"input_schema"`
}

// InputSchema 输入模式定义
type InputSchema struct {
	Type       string                 `json:"type"`
	Required   []string               `json:"required"`
	Properties map[string]interface{} `json:"properties"`
}

// ChatResponse 聊天响应（匹配 Anthropic Messages API 实际格式）
type ChatResponse struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
	Usage   Usage          `json:"usage"`
}

// ContentBlock 内容块（Anthropic 的 tool_use 格式）
type ContentBlock struct {
	Type  string                 `json:"type"`            // "text" 或 "tool_use"
	Text  string                 `json:"text,omitempty"`  // type=text 时
	ID    string                 `json:"id,omitempty"`    // type=tool_use 时的调用 ID
	Name  string                 `json:"name,omitempty"`  // type=tool_use 时的工具名
	Input map[string]interface{} `json:"input,omitempty"` // type=tool_use 时的参数
}

// Usage token 使用量
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// NewClaudeClient 创建 Claude 客户端
func NewClaudeClient(model string) *ClaudeClient {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "WARNING: ANTHROPIC_API_KEY environment variable is not set")
	}
	return &ClaudeClient{
		apiKey:    apiKey,
		model:     model,
		maxTokens: 4096,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Chat 发送聊天请求
func (c *ClaudeClient) Chat(ctx context.Context, messages []Message, tools []Tool) (*ChatResponse, error) {
	if c.apiKey == "" {
		return nil, errors.New("ANTHROPIC_API_KEY is not set")
	}

	req := ChatRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   c.maxTokens,
		Temperature: 0.1,
		Tools:       tools,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, respBody)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("decode response failed: %w", err)
	}

	return &chatResp, nil
}
