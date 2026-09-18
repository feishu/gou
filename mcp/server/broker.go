package server

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrSessionNotFound 会话未找到
	ErrSessionNotFound = errors.New("mcp: session not found")
	// ErrSessionClosed 会话已关闭
	ErrSessionClosed = errors.New("mcp: session is closed")
	// ErrSessionBufferFull 会话消息缓冲队列已满
	ErrSessionBufferFull = errors.New("mcp: session message buffer full")
)

// Session 表示单个 MCP 会话（如 SSE 长连接通道）
type Session interface {
	ID() string
	Send(msg []byte) error
	Receive() <-chan []byte
	Close() error
	IsClosed() bool
	SetMeta(key string, val interface{})
	GetMeta(key string) (interface{}, bool)
}

// SessionBroker 会话总线管理器接口
type SessionBroker interface {
	CreateSession(id ...string) (Session, error)
	GetSession(id string) (Session, bool)
	RemoveSession(id string)
	Close() error
}

type defaultSession struct {
	id     string
	ch     chan []byte
	closed bool
	meta   map[string]interface{}
	mu     sync.RWMutex
}

// newDefaultSession 创建单个会话实例，默认缓冲区容量 64
func newDefaultSession(id string, bufferSize ...int) *defaultSession {
	capSize := 64
	if len(bufferSize) > 0 && bufferSize[0] > 0 {
		capSize = bufferSize[0]
	}
	return &defaultSession{
		id:   id,
		ch:   make(chan []byte, capSize),
		meta: make(map[string]interface{}),
	}
}

func (s *defaultSession) ID() string {
	return s.id
}

func (s *defaultSession) SetMeta(key string, val interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.meta == nil {
		s.meta = make(map[string]interface{})
	}
	s.meta[key] = val
}

func (s *defaultSession) GetMeta(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.meta == nil {
		return nil, false
	}
	val, ok := s.meta[key]
	return val, ok
}

// Send 向会话消息队列发送数据。
// 通过 RLock 互斥保证在向 channel 写入时 channel 绝不会被并发 close，彻底杜绝 panic: send on closed channel
func (s *defaultSession) Send(msg []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrSessionClosed
	}

	select {
	case s.ch <- msg:
		return nil
	default:
		return ErrSessionBufferFull
	}
}

// Receive 返回用于消费消息的只读 channel
func (s *defaultSession) Receive() <-chan []byte {
	return s.ch
}

// Close 幂等安全关闭会话。
// 加写锁排空或阻断后续 Send，安全关闭底通信道
func (s *defaultSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	close(s.ch)
	return nil
}

func (s *defaultSession) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// defaultSessionBroker 默认线程安全会话总线实现
type defaultSessionBroker struct {
	sessions sync.Map // map[string]Session
}

// NewDefaultSessionBroker 创建默认的 SessionBroker 实例
func NewDefaultSessionBroker() SessionBroker {
	return &defaultSessionBroker{}
}

func (b *defaultSessionBroker) CreateSession(id ...string) (Session, error) {
	var sessionID string
	if len(id) > 0 && id[0] != "" {
		sessionID = id[0]
	} else {
		sessionID = fmt.Sprintf("mcp_%d", time.Now().UnixNano())
	}

	sess := newDefaultSession(sessionID)
	b.sessions.Store(sessionID, sess)
	return sess, nil
}

func (b *defaultSessionBroker) GetSession(id string) (Session, bool) {
	if val, ok := b.sessions.Load(id); ok {
		if sess, ok := val.(Session); ok {
			return sess, true
		}
	}
	return nil, false
}

func (b *defaultSessionBroker) RemoveSession(id string) {
	if val, ok := b.sessions.LoadAndDelete(id); ok {
		if sess, ok := val.(Session); ok {
			_ = sess.Close()
		}
	}
}

func (b *defaultSessionBroker) Close() error {
	b.sessions.Range(func(key, value interface{}) bool {
		if sess, ok := value.(Session); ok {
			_ = sess.Close()
		}
		b.sessions.Delete(key)
		return true
	})
	return nil
}
