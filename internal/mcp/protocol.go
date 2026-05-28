package mcp

// JSONRPCRequest JSON-RPC 2.0 请求结构
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse JSON-RPC 2.0 响应结构
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError JSON-RPC 错误
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// InitializeParams MCP 初始化参数
type InitializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    Capability `json:"capabilities"`
	ClientInfo      ClientInfo `json:"clientInfo"`
}

// Capability 能力声明
type Capability struct {
	Tools *ToolCapability `json:"tools,omitempty"`
}

// ToolCapability 工具能力
type ToolCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ClientInfo 客户端信息
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerInfo 服务端信息
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult 初始化响应
type InitializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    Capability `json:"capabilities"`
	ServerInfo      ServerInfo `json:"serverInfo"`
}

// ToolInfo 工具信息
type ToolInfo struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema 输入模式
type InputSchema struct {
	Type       string                 `json:"type"`
	Required   []string               `json:"required,omitempty"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

// CallToolParams 工具调用参数
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolResult 工具调用结果
type ToolResult struct {
	Content []ToolResultContent `json:"content"`
	IsError bool                `json:"isError,omitempty"`
}

// ToolResultContent 工具结果内容
type ToolResultContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
