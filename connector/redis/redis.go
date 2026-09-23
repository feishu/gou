package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/gou/connector/base"
	"github.com/yaoapp/gou/helper"
	"github.com/yaoapp/gou/types"
	"github.com/yaoapp/kun/any"
)

// Connector connector
type Connector struct {
	base.NonSQL
	id      string
	file    string
	Name    string        `json:"name"`
	Rdb     *redis.Client `json:"-"`
	Options Options       `json:"options"`
	types.MetaInfo
}

// Options the redis connector option
type Options struct {
	Host    string `json:"host,omitempty"`
	Port    string `json:"port,omitempty"`
	User    string `json:"user,omitempty"`
	Pass    string `json:"pass,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
	DB      string `json:"db,omitempty"`
}

// Register the connections from dsl
func (r *Connector) Register(file string, id string, dsl []byte) error {

	r.id = id
	r.file = file

	err := application.Parse(file, dsl, r)
	if err != nil {
		return err
	}

	err = r.setDefaults()
	if err != nil {
		return err
	}

	return r.makeConnection()
}

// Is the connections from dsl
func (r *Connector) Is(typ int) bool {
	return 2 == typ
}

// ID get connector id
func (r *Connector) ID() string {
	return r.id
}

// Close connections
func (r *Connector) Close() error {
	return r.Rdb.Close()
}

func (r *Connector) setDefaults() error {
	r.Options.Host = helper.EnvString(r.Options.Host)
	r.Options.Pass = helper.EnvString(r.Options.Pass)
	r.Options.User = helper.EnvString(r.Options.User)
	r.Options.Port = helper.EnvString(r.Options.Port)
	r.Options.DB = helper.EnvString(r.Options.DB)
	r.Options.Timeout = helper.EnvInt(r.Options.Timeout, 5)
	if r.Options.Timeout == 0 {
		r.Options.Timeout = 5
	}

	if r.Options.Port == "" {
		r.Options.Port = "6379"
	}

	if r.Options.DB == "" {
		r.Options.DB = "0"
	}

	return nil
}

func (r *Connector) makeConnection() error {
	if r.Options.Host == "" {
		return fmt.Errorf("options.host is required")
	}

	timeout := time.Duration(r.Options.Timeout) * time.Second
	options := &redis.Options{
		Addr:         fmt.Sprintf("%s:%s", r.Options.Host, r.Options.Port),
		DB:           any.Of(r.Options.DB).CInt(),
		DialTimeout:  timeout,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}

	if r.Options.User != "" {
		options.Username = r.Options.User
	}

	if r.Options.Pass != "" {
		options.Password = r.Options.Pass
	}

	client := redis.NewClient(options)
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.Ping(pingCtx).Result()
	if err != nil {
		return err
	}

	r.Rdb = client
	return nil
}

// Setting get the connection setting
func (r *Connector) Setting() map[string]interface{} {
	return map[string]interface{}{
		"db":      r.Options.DB,
		"host":    r.Options.Host,
		"port":    r.Options.Port,
		"user":    r.Options.User,
		"pass":    r.Options.Pass,
		"timeout": r.Options.Timeout,
	}
}

// GetMetaInfo returns the meta information
func (r *Connector) GetMetaInfo() types.MetaInfo {
	return r.MetaInfo
}
