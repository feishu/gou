package server

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSessionBrokerLifecycle(t *testing.T) {
	broker := NewDefaultSessionBroker()
	defer broker.Close()

	sess, err := broker.CreateSession("test_session_1")
	assert.NoError(t, err)
	assert.Equal(t, "test_session_1", sess.ID())
	assert.False(t, sess.IsClosed())

	// 验证获取
	foundSess, ok := broker.GetSession("test_session_1")
	assert.True(t, ok)
	assert.Equal(t, sess.ID(), foundSess.ID())

	// 发送消息
	err = sess.Send([]byte("hello mcp"))
	assert.NoError(t, err)

	// 接收消息
	select {
	case msg := <-sess.Receive():
		assert.Equal(t, "hello mcp", string(msg))
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for message")
	}

	// 移除并关闭
	broker.RemoveSession("test_session_1")
	assert.True(t, sess.IsClosed())

	_, ok = broker.GetSession("test_session_1")
	assert.False(t, ok)

	// 已关闭后发送应该报错而不是 panic
	err = sess.Send([]byte("after close"))
	assert.Equal(t, ErrSessionClosed, err)
}

func TestSessionIdempotentClose(t *testing.T) {
	sess := newDefaultSession("idempotent_test")
	assert.NoError(t, sess.Close())
	assert.True(t, sess.IsClosed())

	// 二次关闭无 panic 无报错
	assert.NoError(t, sess.Close())
	assert.True(t, sess.IsClosed())
}

func TestSessionBufferFull(t *testing.T) {
	// 容量为 2 的缓冲队列
	sess := newDefaultSession("buffer_test", 2)
	defer sess.Close()

	assert.NoError(t, sess.Send([]byte("msg1")))
	assert.NoError(t, sess.Send([]byte("msg2")))

	// 队列满时必须返回 ErrSessionBufferFull 而不是静默丢弃
	err := sess.Send([]byte("msg3"))
	assert.Equal(t, ErrSessionBufferFull, err)
}

// TestSessionConcurrentSendAndClose 验证高并发下 Send 与 Close 并行，绝无 panic: send on closed channel
func TestSessionConcurrentSendAndClose(t *testing.T) {
	for round := 0; round < 10; round++ {
		sess := newDefaultSession("concurrent_test", 100)
		var wg sync.WaitGroup

		// 启动 50 个 goroutine 并发发送
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					_ = sess.Send([]byte("ping"))
					time.Sleep(10 * time.Microsecond)
				}
			}()
		}

		// 在发送中途随机触发 Close
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Microsecond)
			_ = sess.Close()
		}()

		wg.Wait()
		assert.True(t, sess.IsClosed())
	}
}
