package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yaoapp/gou/websocket"
	"github.com/yaoapp/kun/log"
)

// New create a new http server
func New(router *gin.Engine, option Option) *Server {

	if option.Timeout == 0 {
		option.Timeout = 5 * time.Second
	}

	if option.DrainTimeout == 0 {
		option.DrainTimeout = 15 * time.Second
	}

	if option.ReadHeaderTimeout == 0 {
		option.ReadHeaderTimeout = 5 * time.Second
	}

	if option.IdleTimeout == 0 {
		option.IdleTimeout = 120 * time.Second
	}

	server := &Server{
		option: &option,
		signal: make(chan uint8, 1),
		event:  make(chan uint8, 1),
		status: CREATED,
	}
	if router != nil {
		server.router.Store(router)
	}
	return server
}

// Reset the http server router
func (server *Server) Reset(router *gin.Engine) {
	server.SwapRouter(router)
}

// SwapRouter atomically swaps the router of the http server without dropping connections
func (server *Server) SwapRouter(router *gin.Engine) {
	server.router.Store(router)
	upgraders := websocket.AllUpgraders()
	for _, upgrader := range upgraders {
		upgrader.SetRouter(router)
	}
}

// Router returns current router
func (server *Server) Router() *gin.Engine {
	return server.router.Load()
}

// Event get event signal
func (server *Server) Event() chan uint8 {
	return server.event
}

// Port get server port
func (server *Server) Port() (int, error) {
	if server.addr == nil {
		return 0, fmt.Errorf("server is not started")
	}
	addr := strings.Split(server.addr.String(), ":")
	if len(addr) != 2 {
		return 0, fmt.Errorf("can't get port %s", server.addr.String())
	}
	port, err := strconv.Atoi(addr[1])
	if err != nil {
		return 0, err
	}
	return port, nil
}

// Ready check if the status is ready
func (server *Server) Ready() bool {
	return server.status == READY
}

// Start a http server
func (server *Server) Start() error {

	switch server.status {
	case READY:
		return fmt.Errorf("server already started")

	case STARTING:
		return fmt.Errorf("server is starting")
	}

	server.status = STARTING

	// Server Setting
	var listener net.Listener
	var err error
	addr := fmt.Sprintf("%s:%d", server.option.Host, server.option.Port)
	listener, err = net.Listen("tcp4", addr)
	if err != nil {
		log.Error("[Server] %s %s", addr, err.Error())
		server.status = CREATED
		server.event <- ERROR
		return err
	}

	// network preparing
	server.addr = listener.Addr()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if curRouter := server.router.Load(); curRouter != nil {
			curRouter.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})

	srv := &http.Server{
		Addr:              server.addr.String(),
		Handler:           handler,
		ReadHeaderTimeout: server.option.ReadHeaderTimeout,
		IdleTimeout:       server.option.IdleTimeout,
	}
	server.srv = srv

	// close server
	defer func() {
		if server.status == RESTARTING {
			return
		}

		log.Info("[Server] %s is shutting down gracefully...", srv.Addr)
		drainTimeout := server.option.DrainTimeout
		if drainTimeout <= 0 {
			drainTimeout = 15 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
		defer cancel()

		err := srv.Shutdown(shutdownCtx)
		if err != nil {
			log.Warn("[Server] %s graceful shutdown warning: %v, forcing close", srv.Addr, err)
			_ = srv.Close()
		}

		server.status = CLOSED
		server.event <- CLOSE
	}()

	// WebSocket
	upgraders := websocket.AllUpgraders()
	if len(upgraders) > 0 {

		// Start WebSocket Hub
		for id, upgrader := range upgraders {
			if r := server.router.Load(); r != nil {
				upgrader.SetRouter(r)
			}
			go upgrader.Start()
			log.Info("Websocket %s start", id)
		}

		// Stop WebSocket Hub
		defer func() {
			for id, upgrader := range upgraders {
				upgrader.Stop()
				log.Info("Websocket %s quit", id)
			}
		}()
	}

	// start server
	go func() {
		server.status = READY
		server.event <- READY
		if errSrv := srv.Serve(listener); errSrv != nil && errSrv != http.ErrServerClosed && !errors.Is(errSrv, net.ErrClosed) {
			err = errSrv
			log.Error("[Server] %s %s", srv.Addr, errSrv.Error())
			server.signal <- ERROR
		}
	}()

	// make a timer
	timer := time.NewTimer(time.Duration(server.option.Timeout))
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			if server.Ready() {
				timer.Stop()
				break
			}

			log.Error("[Server] %s start operation was canceled (timeout %d)", srv.Addr, server.option.Timeout)
			return fmt.Errorf("canceled (timeout %d)", server.option.Timeout)

		case signal := <-server.signal:
			switch signal {

			case READY:
				log.Info("[Server] %s is ready", srv.Addr)
				break

			case CLOSE:
				log.Info("[Server] %s is shutting down gracefully...", srv.Addr)
				drainTimeout := server.option.DrainTimeout
				if drainTimeout <= 0 {
					drainTimeout = 15 * time.Second
				}
				shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
				defer cancel()

				err := srv.Shutdown(shutdownCtx)
				if err != nil {
					log.Warn("[Server] %s graceful shutdown warning: %v, forcing close", srv.Addr, err)
					_ = srv.Close()
				}

				log.Info("[Server] %s gracefully closed", srv.Addr)
				return nil

			case RESTART:
				log.Info("[Server] %s was closed (for restarting)", srv.Addr)
				server.status = RESTARTING

				drainTimeout := server.option.DrainTimeout
				if drainTimeout <= 0 {
					drainTimeout = 15 * time.Second
				}
				shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
				defer cancel()

				err := srv.Shutdown(shutdownCtx)
				if err != nil {
					log.Warn("[Server] %s graceful shutdown warning: %v, forcing close", srv.Addr, err)
					_ = srv.Close()
				}
				_ = listener.Close()

				// drain any stale signal in channel before restarting
				select {
				case <-server.signal:
				default:
				}

				defer server.Start()
				return nil

			case ERROR:
				errMsg := "unknown error"
				if err != nil {
					errMsg = err.Error()
				}
				log.Error("[Server] %s was closed (%s)", srv.Addr, errMsg)
				return err

			default:
				log.Error("[Server] %s was closed (unknown signal %d)", srv.Addr, signal)
				return fmt.Errorf("get an unknown signal %d", signal)
			}
		}
	}
}

// Stop a http server
func (server *Server) Stop() error {
	if server.status != READY {
		return fmt.Errorf("server is not ready")
	}
	server.signal <- CLOSE
	return nil
}

// Restart a http server
func (server *Server) Restart() error {
	if server.status != READY {
		return fmt.Errorf("server is not ready")
	}
	server.signal <- RESTART
	return nil
}

// With middlewares
func (server *Server) With(middlewares ...func(ctx *gin.Context)) *Server {
	if r := server.router.Load(); r != nil {
		for _, middleware := range middlewares {
			r.Use(middleware)
		}
	}
	return server
}
