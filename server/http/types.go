package http

import (
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yaoapp/gou/websocket"
)

const (
	// CREATED the server instance was created
	CREATED = uint8(iota)
	// STARTING the server instance is starting
	STARTING
	// READY the server instance is ready
	READY
	// RESTARTING the server instance is restarting
	RESTARTING
	// CLOSED the server instance was stopped
	CLOSED
)

const (
	// CLOSE close signal
	CLOSE = uint8(iota) + 5
	// RESTART restart signal
	RESTART
	// ERROR error signal
	ERROR
)

// Option the http server opiton
type Option struct {
	Port         int           `json:"port,omitempty"`
	Host         string        `json:"host,omitempty"`
	Timeout           time.Duration `json:"timeout,omitempty"`
	DrainTimeout      time.Duration `json:"drain_timeout,omitempty"`       // 优雅关机排空超时时间，默认 15s
	ReadHeaderTimeout time.Duration `json:"read_header_timeout,omitempty"` // 请求头读取超时，默认 5s
	IdleTimeout       time.Duration `json:"idle_timeout,omitempty"`        // 空闲连接保持超时，默认 120s
	Root              string        `json:"root,omitempty"`                // API Root
	Allows       []string      `json:"allows,omitempty"`        // CORS Domains
}

// Server the http server opiton
type Server struct {
	router    *gin.Engine
	addr      net.Addr
	srv       *http.Server
	signal    chan uint8
	event     chan uint8
	status    uint8
	option    *Option
	upgraders map[string]*websocket.Upgrader
}
