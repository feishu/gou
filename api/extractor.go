package api

import (
	"errors"
	"fmt"
	nethttp "net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yaoapp/gou/session"
	"github.com/yaoapp/gou/types"
	"github.com/yaoapp/kun/exception"
	"github.com/yaoapp/kun/maps"
)

// ParamOpCode 参数提取操作码
type ParamOpCode uint8

const (
	// OpExtractConstant 静态常量
	OpExtractConstant ParamOpCode = iota
	// OpExtractBody 提取请求原始 Body
	OpExtractBody
	// OpExtractFullPath 提取请求 FullPath
	OpExtractFullPath
	// OpExtractHeaders 提取请求所有 Header
	OpExtractHeaders
	// OpExtractPayload 提取请求 Payload
	OpExtractPayload
	// OpExtractURLParams 提取 URL 查询参数为 types.QueryParam (:params, :query-param)
	OpExtractURLParams
	// OpExtractQueryValues 提取所有 Query 参数 url.Values (:query)
	OpExtractQueryValues
	// OpExtractFormValues 提取所有 Form 参数 url.Values (:form)
	OpExtractFormValues
	// OpExtractContext 提取 Gin Context
	OpExtractContext
	// OpExtractForm 提取 Form 单个字段
	OpExtractForm
	// OpExtractParam 提取单个路径参数
	OpExtractParam
	// OpExtractQuery 提取单个 Query 参数
	OpExtractQuery
	// OpExtractPayloadField 提取 Payload 单字段
	OpExtractPayloadField
	// OpExtractSession 提取 Session 字段
	OpExtractSession
	// OpExtractHeader 提取单个 Header
	OpExtractHeader
	// OpExtractFile 提取上传文件
	OpExtractFile
)

// ParamExtractorStep 单个参数提取步骤
type ParamExtractorStep struct {
	Op    ParamOpCode
	Key   string
	Const interface{}
}

// ExtractorPlan 编译后的参数提取执行计划
type ExtractorPlan []ParamExtractorStep

// CompileExtractorPlan 在 API 加载编译期将 in 参数转化为强类型的静态执行计划
func CompileExtractorPlan(in []interface{}) ExtractorPlan {
	plan := make(ExtractorPlan, 0, len(in))
	for _, value := range in {
		v, ok := value.(string)
		if !ok {
			plan = append(plan, ParamExtractorStep{Op: OpExtractConstant, Const: value})
			continue
		}

		switch v {
		case ":body":
			plan = append(plan, ParamExtractorStep{Op: OpExtractBody})
		case ":fullpath":
			plan = append(plan, ParamExtractorStep{Op: OpExtractFullPath})
		case ":headers":
			plan = append(plan, ParamExtractorStep{Op: OpExtractHeaders})
		case ":payload":
			plan = append(plan, ParamExtractorStep{Op: OpExtractPayload})
		case ":params", ":query-param":
			plan = append(plan, ParamExtractorStep{Op: OpExtractURLParams})
		case ":query":
			plan = append(plan, ParamExtractorStep{Op: OpExtractQueryValues})
		case ":form":
			plan = append(plan, ParamExtractorStep{Op: OpExtractFormValues})
		case ":context":
			plan = append(plan, ParamExtractorStep{Op: OpExtractContext})
		default:
			arg := strings.Split(v, ".")
			length := len(arg)
			if length == 2 {
				switch arg[0] {
				case "$form":
					plan = append(plan, ParamExtractorStep{Op: OpExtractForm, Key: arg[1]})
				case "$param":
					plan = append(plan, ParamExtractorStep{Op: OpExtractParam, Key: arg[1]})
				case "$query":
					plan = append(plan, ParamExtractorStep{Op: OpExtractQuery, Key: arg[1]})
				case "$payload":
					plan = append(plan, ParamExtractorStep{Op: OpExtractPayloadField, Key: arg[1]})
				case "$session":
					plan = append(plan, ParamExtractorStep{Op: OpExtractSession, Key: arg[1]})
				case "$header":
					plan = append(plan, ParamExtractorStep{Op: OpExtractHeader, Key: arg[1]})
				case "$file":
					plan = append(plan, ParamExtractorStep{Op: OpExtractFile, Key: arg[1]})
				default:
					plan = append(plan, ParamExtractorStep{Op: OpExtractConstant, Const: v})
				}
			} else {
				plan = append(plan, ParamExtractorStep{Op: OpExtractConstant, Const: v})
			}
		}
	}
	return plan
}

// Execute 运行时提取参数（固定切片大小，扁平循环，零中间闭包分配）
func (plan ExtractorPlan) Execute(c *gin.Context) []interface{} {
	if len(plan) == 0 {
		return []interface{}{}
	}
	values := make([]interface{}, len(plan))
	for i, step := range plan {
		switch step.Op {
		case OpExtractBody:
			if c.Request.Body == nil {
				values[i] = ""
				continue
			}
			rawBytes, err := ReadBodyBytes(c)
			if err != nil {
				var maxBytesErr *nethttp.MaxBytesError
				if errors.As(err, &maxBytesErr) {
					exception.New("Request entity too large, max body size is %d bytes", 413, MaxBodySize).Throw()
				}
				panic(err)
			}
			values[i] = string(rawBytes)

		case OpExtractFullPath:
			values[i] = c.FullPath()

		case OpExtractHeaders:
			values[i] = c.Request.Header

		case OpExtractPayload:
			value, has := c.Get("__payloads")
			if !has {
				values[i] = maps.MapStr{}
				continue
			}
			valueMap, ok := value.(map[string]interface{})
			if !ok {
				values[i] = maps.MapStr{}
				continue
			}
			values[i] = valueMap

		case OpExtractURLParams:
			values[i] = types.URLToQueryParam(c.Request.URL.Query())

		case OpExtractQueryValues:
			values[i] = c.Request.URL.Query()

		case OpExtractFormValues:
			values[i] = c.Request.PostForm

		case OpExtractContext:
			values[i] = c

		case OpExtractForm:
			values[i] = c.PostForm(step.Key)

		case OpExtractParam:
			values[i] = c.Param(step.Key)

		case OpExtractQuery:
			values[i] = c.Query(step.Key)

		case OpExtractPayloadField:
			if payloads, has := c.Get("__payloads"); has {
				if value, has := payloads.(map[string]interface{})[step.Key]; has {
					values[i] = value
					continue
				}
			}
			values[i] = ""

		case OpExtractSession:
			if sid := c.GetString("__sid"); sid != "" {
				name := step.Key
				var cache map[string]interface{}
				if rawCache, exists := c.Get("__session_cache"); exists {
					if m, ok := rawCache.(map[string]interface{}); ok {
						cache = m
					}
				}
				if cache == nil {
					cache = make(map[string]interface{})
					c.Set("__session_cache", cache)
				}
				if val, ok := cache[name]; ok {
					values[i] = val
					continue
				}
				val := session.Global().ID(sid).MustGet(name)
				cache[name] = val
				values[i] = val
				continue
			}
			values[i] = ""

		case OpExtractHeader:
			values[i] = c.GetHeader(step.Key)

		case OpExtractFile:
			file, err := c.FormFile(step.Key)
			if err != nil {
				values[i] = types.UploadFile{Error: fmt.Sprintf("%s %s", step.Key, err.Error())}
				continue
			}
			ext := filepath.Ext(file.Filename)
			dir, err := os.MkdirTemp("", "upload")
			if err != nil {
				values[i] = types.UploadFile{Error: fmt.Sprintf("%s %s", step.Key, err.Error())}
				continue
			}
			tmpfile, err := os.CreateTemp(dir, fmt.Sprintf("file-*%s", ext))
			if err != nil {
				values[i] = types.UploadFile{Error: fmt.Sprintf("%s %s", step.Key, err.Error())}
				continue
			}
			_ = tmpfile.Close()
			if err := c.SaveUploadedFile(file, tmpfile.Name()); err != nil {
				values[i] = types.UploadFile{Error: fmt.Sprintf("%s %s", step.Key, err.Error())}
				continue
			}
			values[i] = types.UploadFile{
				UID:      c.GetHeader("Content-Uid"),
				Range:    c.GetHeader("Content-Range"),
				Sync:     c.GetHeader("Content-Sync") == "true",
				Name:     file.Filename,
				TempFile: tmpfile.Name(),
				Size:     file.Size,
				Header:   file.Header,
			}

		case OpExtractConstant:
			values[i] = step.Const
		}
	}
	return values
}
