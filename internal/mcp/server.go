package mcp

import (
	"fmt"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

const (
	// heartbeatInterval SSE 心跳间隔，防止连接超时断开
	heartbeatInterval = 30 * time.Second
)

// SSEServer 封装 MCP SSE 服务器
type SSEServer struct {
	mcpServer *server.MCPServer
	mux       *http.ServeMux
}

// NewSSEServer 创建 SSE 服务器
func NewSSEServer(mcpServer *server.MCPServer) *SSEServer {
	s := &SSEServer{
		mcpServer: mcpServer,
		mux:       http.NewServeMux(),
	}
	s.setupRoutes()
	return s
}

func (s *SSEServer) setupRoutes() {
	// SSE 端点 - 客户端通过此端点建立 SSE 连接
	s.mux.HandleFunc("/sse", s.handleSSE)
	// 健康检查
	s.mux.HandleFunc("/health", s.handleHealth)
}

func (s *SSEServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// 发送初始连接事件
	_, _ = fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"connected\"}\n\n")
	flusher.Flush()

	// 定时发送心跳，防止连接超时断开
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, err := fmt.Fprintf(w, ": heartbeat\n\n")
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *SSEServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// ServeHTTP 实现 http.Handler 接口
func (s *SSEServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
