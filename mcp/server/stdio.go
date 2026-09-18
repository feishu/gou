package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/yaoapp/kun/log"
)

// ServeStdio 在指定的输入输出流上运行 MCP JSON-RPC 2.0 协议循环（常用于管道、命令行与本地进程间通信）
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}

	scanner := bufio.NewScanner(in)
	// 允许最大 16MB 的单条 JSON 报文
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 16*1024*1024)

	var writeMu sync.Mutex

	// 构造 stdio 模式的上下文信息
	stdioReqInfo := &RequestInfo{
		Header: make(http.Header),
		Query:  make(map[string][]string),
		Remote: "stdio://local",
		Path:   "stdio",
	}
	stdioCtx := WithRequestInfo(ctx, stdioReqInfo)

	// 监听 context 取消
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			// 上下文已取消，可由外部关闭 in 或主动退出
		case <-done:
		}
	}()

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// 执行 JSON-RPC 分发
		resp, err := s.DispatchJSONRPC(stdioCtx, line)
		if err != nil {
			log.Error("[MCP-Stdio] Dispatch error: %v", err)
		}

		// 通知类报文无响应，仅在有 resp 时回写
		if resp != nil {
			respBytes, encErr := json.Marshal(resp)
			if encErr != nil {
				log.Error("[MCP-Stdio] Marshal response error: %v", encErr)
				continue
			}

			writeMu.Lock()
			_, writeErr := fmt.Fprintf(out, "%s\n", string(respBytes))
			writeMu.Unlock()

			if writeErr != nil {
				return fmt.Errorf("failed to write stdio response: %w", writeErr)
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil && scanErr != io.EOF {
		return fmt.Errorf("stdio scanner error: %w", scanErr)
	}

	return nil
}

// RunStdio 使用系统标准输入输出（os.Stdin / os.Stdout）启动 MCP 管道服务，自动处理 SIGINT/SIGTERM 优雅停机
func (s *Server) RunStdio(ctx ...context.Context) error {
	var runCtx context.Context
	var cancel context.CancelFunc

	if len(ctx) > 0 && ctx[0] != nil {
		runCtx, cancel = context.WithCancel(ctx[0])
	} else {
		runCtx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	go func() {
		select {
		case <-sigChan:
			cancel()
		case <-runCtx.Done():
		}
	}()

	return s.ServeStdio(runCtx, os.Stdin, os.Stdout)
}
