package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"muitl-agent/internal/agent"
	mcpserver "muitl-agent/internal/mcp"

	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// 创建 MCP Server
	s := server.NewMCPServer(
		"Go AI Agent",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// 注册工具
	agent.RegisterTools(s)

	// 启动 SSE 服务器
	sseServer := mcpserver.NewSSEServer(s)

	addr := ":8080"
	log.Printf("🚀 MCP Server starting on %s", addr)

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      sseServer,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE 需要长连接，不设写超时
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("👋 MCP Server shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}
}
