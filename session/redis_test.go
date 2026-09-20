package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var redisAvailable = false

func init() {
	host := os.Getenv("GOU_TEST_REDIS_HOST")
	port := os.Getenv("GOU_TEST_REDIS_PORT")
	db := os.Getenv("GOU_TEST_REDIS_DB")
	pass := os.Getenv("GOU_TEST_REDIS_PASSWORD")

	if host == "" {
		host = "127.0.0.1"
	}

	args := []string{}
	if port != "" {
		args = append(args, port)
	}

	if db != "" {
		args = append(args, db)
	}

	if pass != "" {
		args = append(args, pass)
	}

	rdb, err := NewRedis(host, args...)
	if err == nil {
		Register("redis", rdb)
		redisAvailable = true
	}
}

func checkRedis(t *testing.T) {
	if !redisAvailable {
		t.Skip("Redis is not available, skipping redis tests")
	}
}

func TestRedisMake(t *testing.T) {
	checkRedis(t)
	s := Use("redis").Make().Expire(3600 * time.Second).AsGlobal()
	assert.NotNil(t, s.GetID())
}

func TestRedisID(t *testing.T) {
	checkRedis(t)
	id := ID()
	s := Use("redis").ID(id)
	assert.Equal(t, id, s.GetID())
}

func TestRedisMustSetGetDel(t *testing.T) {
	checkRedis(t)
	id := ID()
	s := Use("redis").ID(id).Expire(200 * time.Millisecond)
	s.MustSet("foo", "bar")
	v := s.MustGet("foo")
	assert.Equal(t, "bar", v)

	s.MustSetMany(map[string]interface{}{"hello": "world", "hi": "gou"})
	assert.Equal(t, "world", s.MustGet("hello"))
	assert.Equal(t, "gou", s.MustGet("hi"))

	s.MustDel("hi")
	assert.Nil(t, s.MustGet("hi"))

	time.Sleep(201 * time.Millisecond)
	assert.Nil(t, s.MustGet("foo"))
	assert.Nil(t, s.MustGet("hello"))
	assert.Nil(t, s.MustGet("hi"))
}

func TestRedisMustSetWithEx(t *testing.T) {
	checkRedis(t)
	id := ID()
	ss := Use("redis").ID(id)
	ss.MustSetWithEx("foo", "bar", 200*time.Millisecond)
	assert.Equal(t, "bar", ss.MustGet("foo"))

	ss.MustSetManyWithEx(map[string]interface{}{"hello": "world", "hi": "gou"}, 200*time.Millisecond)
	assert.Equal(t, "world", ss.MustGet("hello"))
	assert.Equal(t, "gou", ss.MustGet("hi"))

	time.Sleep(210 * time.Millisecond)
	assert.Nil(t, ss.MustGet("foo"))
	assert.Nil(t, ss.MustGet("hello"))
	assert.Nil(t, ss.MustGet("hi"))
}

func TestRedisMustDump(t *testing.T) {
	checkRedis(t)
	id := ID()
	ss := Use("redis").ID(id).Expire(200 * time.Millisecond)
	ss.MustSet("foo", "bar")
	ss.MustSet("hello", "world")

	data := ss.MustDump()
	assert.Equal(t, "bar", data["foo"])
	assert.Equal(t, "world", data["hello"])

	time.Sleep(201 * time.Millisecond)
	data = ss.MustDump()
	assert.Equal(t, map[string]interface{}{}, data)
}

func TestRedisBatchOperations(t *testing.T) {
	checkRedis(t)
	id := ID()
	ss := Use("redis").ID(id).Expire(10 * time.Second)

	// 批量设置
	err := ss.SetMany(map[string]interface{}{
		"k1": "v1",
		"k2": "v2",
		"k3": 123,
	})
	assert.NoError(t, err)

	// 批量读取
	vals, err := ss.GetMany([]string{"k1", "k2", "k3", "k4"})
	assert.NoError(t, err)
	assert.Equal(t, "v1", vals["k1"])
	assert.Equal(t, "v2", vals["k2"])
	assert.Equal(t, float64(123), vals["k3"]) // json反序列化数字为 float64
	assert.Nil(t, vals["k4"])

	// 批量删除
	err = ss.DelMany([]string{"k1", "k2"})
	assert.NoError(t, err)
	assert.Nil(t, ss.MustGet("k1"))
	assert.Nil(t, ss.MustGet("k2"))
	assert.Equal(t, float64(123), ss.MustGet("k3"))
}

func TestRedisOpContext(t *testing.T) {
	r := &Redis{timeout: 50 * time.Millisecond}
	ctx, cancel := r.opContext()
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("Context should not be done immediately")
	default:
	}

	time.Sleep(60 * time.Millisecond)
	select {
	case <-ctx.Done():
		assert.Equal(t, context.DeadlineExceeded, ctx.Err())
	default:
		t.Fatal("Context should be timed out")
	}

	// 验证预取消父 Context 传播
	parentCtx, parentCancel := context.WithCancel(context.Background())
	parentCancel()
	childCtx, childCancel := r.opContext(parentCtx)
	defer childCancel()
	assert.Equal(t, context.Canceled, childCtx.Err())
}

