package server

import (
	"strings"
	"sync"
)

var (
	servers = make(map[string]*Server)
	mu      sync.RWMutex
)

// GetServer 获取或创建指定分组的 MCP Server 实例
func GetServer(group string) *Server {
	cleanGroup := normalizeGroup(group)

	mu.RLock()
	s, ok := servers[cleanGroup]
	mu.RUnlock()
	if ok {
		return s
	}

	mu.Lock()
	defer mu.Unlock()
	// Double check
	if s, ok := servers[cleanGroup]; ok {
		return s
	}

	serverName := "yao-mcp-" + cleanGroup
	s = NewServer(serverName, "1.0.0")
	servers[cleanGroup] = s
	return s
}

// ListGroups 列出所有已注册的 MCP 分组
func ListGroups() []string {
	mu.RLock()
	defer mu.RUnlock()

	groups := make([]string, 0, len(servers))
	for g := range servers {
		groups = append(groups, g)
	}
	return groups
}

// Reset 重置所有已注册的 MCP Server（测试隔离使用）
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	servers = make(map[string]*Server)
}

func normalizeGroup(group string) string {
	g := strings.Trim(strings.TrimSpace(group), "/")
	if g == "" {
		return "default"
	}
	g = strings.ReplaceAll(g, "/", "_")
	g = strings.ReplaceAll(g, "-", "_")
	return strings.ToLower(g)
}
