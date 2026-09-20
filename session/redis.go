package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	jsoniter "github.com/json-iterator/go"
	"github.com/yaoapp/kun/any"
	"github.com/yaoapp/kun/log"
)

// Redis session store
type Redis struct {
	timeout time.Duration
	options *redis.Options
	rdb     *redis.Client
}

// NewRedis create a new redis instance
// host string, port int, db int, password string, username string, timeout int
func NewRedis(host string, options ...string) (*Redis, error) {

	inst := &Redis{
		timeout: 5 * time.Second,
		options: &redis.Options{},
		rdb:     nil,
	}

	port := 6379
	if len(options) > 0 {
		port = any.Of(options[0]).CInt()
	}

	if len(options) > 1 {
		inst.options.DB = any.Of(options[1]).CInt()
	}

	if len(options) > 2 {
		inst.options.Password = options[2]
	}

	if len(options) > 3 {
		inst.options.Username = options[3]
	}

	if len(options) > 4 {
		inst.timeout = time.Duration(any.Of(options[4]).CInt()) * time.Second
	}

	inst.options.Addr = fmt.Sprintf("%s:%d", host, port)

	client := redis.NewClient(inst.options).WithTimeout(inst.timeout)
	pingCtx, pingCancel := inst.opContext()
	defer pingCancel()
	_, err := client.Ping(pingCtx).Result()
	if err != nil {
		log.Error("Session redis Ping: %s host: %s options: %v", err.Error(), host, options)
		return nil, err
	}

	inst.rdb = client
	return inst, nil
}

// opContext 构建受保护的带超时 Context
func (redis *Redis) opContext(parent ...context.Context) (context.Context, context.CancelFunc) {
	p := context.Background()
	if len(parent) > 0 && parent[0] != nil {
		p = parent[0]
	}
	t := redis.timeout
	if t <= 0 {
		t = 5 * time.Second
	}
	return context.WithTimeout(p, t)
}

// Init initialization
func (redis *Redis) Init() {}

// Set session value (基于 Redis Hash 聚合存储，1 RTT)
func (redis *Redis) Set(id string, key string, value interface{}, timeout time.Duration) error {
	return redis.SetWithContext(context.Background(), id, key, value, timeout)
}

// SetWithContext 携带父 Context 的会话写入方法
func (redis *Redis) SetWithContext(parentCtx context.Context, id string, key string, value interface{}, timeout time.Duration) error {
	hkey := fmt.Sprintf("yao:session:%s", id)
	bytes, err := jsoniter.Marshal(value)
	if err != nil {
		log.Error("Session redis Set: %s key %s", err.Error(), key)
		return err
	}

	log.Debug("Session redis Set: %s KEY: %s VALUE: %v TS: %#v", hkey, key, value, timeout)

	ctx, cancel := redis.opContext(parentCtx)
	defer cancel()

	pipe := redis.rdb.Pipeline()
	pipe.HSet(ctx, hkey, key, bytes)
	if timeout > 0 {
		pipe.Expire(ctx, hkey, timeout)
	}

	// 顺带清理旧格式离散 String key，防止残留陈旧脏数据
	skey := fmt.Sprintf("yao:session:%s:%s", id, key)
	pipe.Del(ctx, skey)

	_, err = pipe.Exec(ctx)
	if err != nil {
		log.Error("Session redis Set: %s", err.Error())
		return err
	}
	return nil
}

// Get session value (支持 Hash 优先 + 旧离散 String 双读回退与惰性迁移)
func (redis *Redis) Get(id string, key string) (interface{}, error) {
	return redis.GetWithContext(context.Background(), id, key)
}

// GetWithContext 携带父 Context 的会话读取方法
func (redis *Redis) GetWithContext(parentCtx context.Context, id string, key string) (interface{}, error) {
	hkey := fmt.Sprintf("yao:session:%s", id)
	ctx, cancel := redis.opContext(parentCtx)
	defer cancel()

	// 1. 优先尝试从 Hash 读取
	val, err := redis.rdb.HGet(ctx, hkey, key).Result()
	if err == nil {
		var value interface{}
		err = jsoniter.Unmarshal([]byte(val), &value)
		if err != nil {
			log.Error("Session redis Get JSON: %s val: %s ERROR:%s", key, val, err.Error())
			return nil, err
		}
		return value, nil
	}

	// 若并非不存在（例如网络或连接错误），直接返回错误
	if err != nil && err.Error() != "redis: nil" && !strings.Contains(err.Error(), "redis: nil") {
		log.Error("Session redis HGet: %s field: %s ERROR:%s", hkey, key, err.Error())
		return nil, err
	}

	// 2. Hash 未命中，双读回退：读取旧离散 String 格式 (yao:session:{id}:{key})
	skey := fmt.Sprintf("yao:session:%s:%s", id, key)
	oldVal, oldErr := redis.rdb.Get(ctx, skey).Result()
	if oldErr != nil {
		if oldErr.Error() == "redis: nil" || strings.Contains(oldErr.Error(), "redis: nil") {
			return nil, nil // 两者都未命中
		}
		log.Error("Session redis fallback Get: %s ERROR:%s", skey, oldErr.Error())
		return nil, oldErr
	}

	// 3. 旧格式命中，反序列化并执行惰性迁移至 Hash
	var value interface{}
	err = jsoniter.Unmarshal([]byte(oldVal), &value)
	if err != nil {
		log.Error("Session redis fallback Get JSON: %s val: %s ERROR:%s", skey, oldVal, err.Error())
		return nil, err
	}

	// 惰性迁移：写入 Hash 并保留原有 TTL，删除旧 String
	ttl, _ := redis.rdb.TTL(ctx, skey).Result()
	pipe := redis.rdb.Pipeline()
	pipe.HSet(ctx, hkey, key, oldVal)
	if ttl > 0 {
		pipe.Expire(ctx, hkey, ttl)
	}
	pipe.Del(ctx, skey)
	_, _ = pipe.Exec(ctx)

	return value, nil
}

// Del session value
func (redis *Redis) Del(id string, key string) error {
	return redis.DelWithContext(context.Background(), id, key)
}

// DelWithContext 携带父 Context 的会话删除方法
func (redis *Redis) DelWithContext(parentCtx context.Context, id string, key string) error {
	hkey := fmt.Sprintf("yao:session:%s", id)
	skey := fmt.Sprintf("yao:session:%s:%s", id, key)
	ctx, cancel := redis.opContext(parentCtx)
	defer cancel()

	log.Debug("Session redis Del: %s field %s", hkey, key)
	pipe := redis.rdb.Pipeline()
	pipe.HDel(ctx, hkey, key)
	pipe.Del(ctx, skey)
	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Error("Session redis Del: %s", err.Error())
		return err
	}
	return nil
}

// Dump session data (使用 HGetAll 替代高危 KEYS 命令，1 RTT 完成全量拉取)
func (redis *Redis) Dump(id string) (map[string]interface{}, error) {
	return redis.DumpWithContext(context.Background(), id)
}

// DumpWithContext 携带父 Context 的全量拉取方法
func (redis *Redis) DumpWithContext(parentCtx context.Context, id string) (map[string]interface{}, error) {
	hkey := fmt.Sprintf("yao:session:%s", id)
	ctx, cancel := redis.opContext(parentCtx)
	defer cancel()

	res := map[string]interface{}{}
	fields, err := redis.rdb.HGetAll(ctx, hkey).Result()
	if err != nil {
		log.Error("Session redis Dump HGetAll %s ERROR:%s", id, err.Error())
		return res, err
	}

	for k, val := range fields {
		var value interface{}
		if err := jsoniter.Unmarshal([]byte(val), &value); err != nil {
			log.Error("Session redis Dump JSON: %s val: %s ERROR:%s", k, val, err.Error())
			res[k] = nil
			continue
		}
		res[k] = value
	}

	// 如果 Hash 中有数据，直接返回
	if len(res) > 0 {
		return res, nil
	}

	// 存量旧数据回退兼容：使用非阻塞 SCAN 替代 KEYS * 规避生产停顿
	prefix := fmt.Sprintf("yao:session:%s:", id)
	var cursor uint64
	for {
		keys, nextCursor, err := redis.rdb.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			log.Error("Session redis Dump Scan %s ERROR:%s", id, err.Error())
			break
		}
		for _, key := range keys {
			pureKey := strings.TrimPrefix(key, prefix)
			val, err := redis.GetWithContext(ctx, id, pureKey)
			if err != nil {
				res[pureKey] = nil
				continue
			}
			res[pureKey] = val
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return res, nil
}

// SetMany 批量设置 session 键值对 (支持 BatchManager 接口，1 RTT Pipeline)
func (redis *Redis) SetMany(id string, values map[string]interface{}, timeout time.Duration) error {
	return redis.SetManyWithContext(context.Background(), id, values, timeout)
}

// SetManyWithContext 携带父 Context 的批量写入方法
func (redis *Redis) SetManyWithContext(parentCtx context.Context, id string, values map[string]interface{}, timeout time.Duration) error {
	if len(values) == 0 {
		return nil
	}
	hkey := fmt.Sprintf("yao:session:%s", id)
	ctx, cancel := redis.opContext(parentCtx)
	defer cancel()

	fields := make(map[string]interface{}, len(values))
	oldKeys := make([]string, 0, len(values))
	for k, v := range values {
		bytes, err := jsoniter.Marshal(v)
		if err != nil {
			log.Error("Session redis SetMany: %s key %s", err.Error(), k)
			return err
		}
		fields[k] = bytes
		oldKeys = append(oldKeys, fmt.Sprintf("yao:session:%s:%s", id, k))
	}

	pipe := redis.rdb.Pipeline()
	pipe.HSet(ctx, hkey, fields)
	if timeout > 0 {
		pipe.Expire(ctx, hkey, timeout)
	}
	if len(oldKeys) > 0 {
		pipe.Del(ctx, oldKeys...)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Error("Session redis SetMany: %s", err.Error())
		return err
	}
	return nil
}

// GetMany 批量读取 session 键值对 (支持 BatchManager 接口，1 RTT HMGet)
func (redis *Redis) GetMany(id string, keys []string) (map[string]interface{}, error) {
	return redis.GetManyWithContext(context.Background(), id, keys)
}

// GetManyWithContext 携带父 Context 的批量读取方法
func (redis *Redis) GetManyWithContext(parentCtx context.Context, id string, keys []string) (map[string]interface{}, error) {
	if len(keys) == 0 {
		return map[string]interface{}{}, nil
	}
	hkey := fmt.Sprintf("yao:session:%s", id)
	ctx, cancel := redis.opContext(parentCtx)
	defer cancel()

	vals, err := redis.rdb.HMGet(ctx, hkey, keys...).Result()
	res := make(map[string]interface{}, len(keys))
	var missingKeys []string

	if err == nil {
		for i, v := range vals {
			key := keys[i]
			if v == nil {
				missingKeys = append(missingKeys, key)
				continue
			}
			strVal, ok := v.(string)
			if !ok {
				missingKeys = append(missingKeys, key)
				continue
			}
			var value interface{}
			if err := jsoniter.Unmarshal([]byte(strVal), &value); err != nil {
				res[key] = nil
				continue
			}
			res[key] = value
		}
	} else {
		missingKeys = keys
	}

	// 对 Hash 中未命中的 key 回退查询旧 String
	for _, mKey := range missingKeys {
		val, err := redis.GetWithContext(ctx, id, mKey)
		if err == nil && val != nil {
			res[mKey] = val
		} else {
			res[mKey] = nil
		}
	}

	return res, nil
}

// DelMany 批量删除 session 键值对 (支持 BatchManager 接口，1 RTT Pipeline)
func (redis *Redis) DelMany(id string, keys []string) error {
	return redis.DelManyWithContext(context.Background(), id, keys)
}

// DelManyWithContext 携带父 Context 的批量删除方法
func (redis *Redis) DelManyWithContext(parentCtx context.Context, id string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	hkey := fmt.Sprintf("yao:session:%s", id)
	ctx, cancel := redis.opContext(parentCtx)
	defer cancel()

	oldKeys := make([]string, len(keys))
	for i, k := range keys {
		oldKeys[i] = fmt.Sprintf("yao:session:%s:%s", id, k)
	}

	pipe := redis.rdb.Pipeline()
	pipe.HDel(ctx, hkey, keys...)
	pipe.Del(ctx, oldKeys...)
	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Error("Session redis DelMany: %s", err.Error())
		return err
	}
	return nil
}

