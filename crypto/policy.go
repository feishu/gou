package crypto

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/gou/query/share"
	"github.com/yaoapp/kun/maps"
)

// FieldPolicyRule 单字段加密策略规则
type FieldPolicyRule struct {
	Table  string `json:"table"`
	Column string `json:"column"`
	Op     string `json:"op"`  // "encrypt" | "hash"
	Alg    string `json:"alg"` // "SM4/CBC/PKCS7" | "SM4/ECB/PKCS7" | "HMAC-SM3"
	Tier   string `json:"tier"`
}

// PolicyData 策略文件结构
type PolicyData struct {
	PolicyVersion interface{}       `json:"policyVersion"`
	Whitelist     []string          `json:"whitelist"`
	Fields        []FieldPolicyRule `json:"fields"`
	Tables        []struct {
		Name   string `json:"name"`
		Model  string `json:"model"`
		Action string `json:"action"`
	} `json:"tables"`
}

// PolicyManager 策略管理器（并发安全，纯内存只读纳秒级匹配）
type PolicyManager struct {
	mu                sync.RWMutex
	tableFields       map[string]map[string]FieldPolicyRule // table -> column -> rule
	modelToTable      map[string]string                     // modelId -> table
	whitelistPrefixes []string                              // 白名单表/模型前缀清单
}

var globalPolicy = &PolicyManager{
	tableFields:  make(map[string]map[string]FieldPolicyRule),
	modelToTable: make(map[string]string),
}

var autoLoadOnce sync.Once

func init() {
	// 注册 Yao 引擎通用策略管理 Process
	process.RegisterGroup("crypto.policy", map[string]process.Handler{
		"load":   processPolicyLoad,
		"get":    processPolicyGet,
		"reload": processPolicyReload,
	})
}

// AutoLoadPolicy 自动发现并载入当前应用/系统中的字段策略配置
func (m *PolicyManager) AutoLoadPolicy() error {
	// 1. 优先读取环境变量指定的策略文件路径
	if path := os.Getenv("YAO_CRYPTO_POLICY_FILE"); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			return m.LoadPolicyJSON(data)
		}
	}

	// 2. 尝试从 application.App 中读取
	if application.App != nil {
		for _, file := range []string{"crypto/field-policy.json", "field-policy.json"} {
			if has, _ := application.App.Exists(file); has {
				if data, err := application.App.Read(file); err == nil {
					return m.LoadPolicyJSON(data)
				}
			}
		}
	}

	// 3. 尝试从当前工作目录及常见工程根路径相对读取
	candidates := []string{
		"crypto/field-policy.json",
		"field-policy.json",
		filepath.Join(".", "crypto", "field-policy.json"),
	}
	if appRoot := os.Getenv("GOU_TEST_APPLICATION"); appRoot != "" {
		candidates = append(candidates, filepath.Join(appRoot, "crypto", "field-policy.json"))
	}
	if yaoRoot := os.Getenv("YAO_ROOT"); yaoRoot != "" {
		candidates = append(candidates, filepath.Join(yaoRoot, "crypto", "field-policy.json"))
	}

	for _, cand := range candidates {
		if data, err := os.ReadFile(cand); err == nil && len(data) > 0 {
			return m.LoadPolicyJSON(data)
		}
	}

	return nil
}

func (m *PolicyManager) ensureLoaded() {
	autoLoadOnce.Do(func() {
		_ = m.AutoLoadPolicy()
	})
}

// RegisterField 注册单个字段策略
func (m *PolicyManager) RegisterField(rule FieldPolicyRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := strings.ToLower(rule.Table)
	col := strings.ToLower(rule.Column)
	if m.tableFields[t] == nil {
		m.tableFields[t] = make(map[string]FieldPolicyRule)
	}
	m.tableFields[t][col] = rule
}

// RegisterModelTable 注册模型与物理表映射
func (m *PolicyManager) RegisterModelTable(modelID, table string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modelToTable[strings.ToLower(modelID)] = strings.ToLower(table)
}

// LoadPolicyJSON 从 JSON 字节流加载策略配置
func (m *PolicyManager) LoadPolicyJSON(data []byte) error {
	var pd PolicyData
	if err := json.Unmarshal(data, &pd); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, w := range pd.Whitelist {
		wLower := strings.ToLower(strings.TrimSpace(w))
		if wLower != "" {
			m.whitelistPrefixes = append(m.whitelistPrefixes, wLower)
		}
	}

	for _, f := range pd.Fields {
		t := strings.ToLower(f.Table)
		col := strings.ToLower(f.Column)
		if m.tableFields[t] == nil {
			m.tableFields[t] = make(map[string]FieldPolicyRule)
		}
		m.tableFields[t][col] = f
	}
	for _, tbl := range pd.Tables {
		if tbl.Model != "" && tbl.Name != "" {
			m.modelToTable[strings.ToLower(tbl.Model)] = strings.ToLower(tbl.Name)
		}
	}
	return nil
}

// LoadPolicyFile 从文件路径加载策略配置
func (m *PolicyManager) LoadPolicyFile(file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	return m.LoadPolicyJSON(data)
}

// RegisterWhitelist 动态注册白名单表或模型前缀
func (m *PolicyManager) RegisterWhitelist(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := strings.ToLower(strings.TrimSpace(prefix))
	if p != "" {
		m.whitelistPrefixes = append(m.whitelistPrefixes, p)
	}
}

// Reset 清空当前所有策略规则（供测试或重新载入使用）
func (m *PolicyManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tableFields = make(map[string]map[string]FieldPolicyRule)
	m.modelToTable = make(map[string]string)
	m.whitelistPrefixes = nil
}

// ResolveTable 解析模型或表名为物理表名
func (m *PolicyManager) ResolveTable(modelOrTable string) string {
	name := strings.ToLower(modelOrTable)
	m.mu.RLock()
	defer m.mu.RUnlock()
	if tbl, ok := m.modelToTable[name]; ok {
		return tbl
	}
	return name
}

// GetFieldRule 获取字段的策略规则
func (m *PolicyManager) GetFieldRule(modelOrTable, column string) (FieldPolicyRule, bool) {
	m.ensureLoaded()
	table := m.ResolveTable(modelOrTable)
	col := strings.ToLower(column)
	m.mu.RLock()
	defer m.mu.RUnlock()
	if cols, ok := m.tableFields[table]; ok {
		rule, found := cols[col]
		return rule, found
	}
	return FieldPolicyRule{}, false
}

// IsWhiteListed 检查表名是否属于白名单内部表，防止递归与死锁
// 优先匹配 field-policy.json 中声明的 whitelist 列表，并保留 Yao DSL 内部虚拟表兜底
func IsWhiteListed(tableOrModel string) bool {
	lower := strings.ToLower(tableOrModel)
	if strings.HasPrefix(lower, "models.") || strings.HasPrefix(lower, "dsl.") {
		return true
	}

	globalPolicy.ensureLoaded()
	globalPolicy.mu.RLock()
	defer globalPolicy.mu.RUnlock()

	for _, prefix := range globalPolicy.whitelistPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// IsEngineCryptoEnabled 全局无缝降级开关
// 优先级: YAO_ENGINE_CRYPTO 环境变量 (off/false/0 严格关闭; on/true/1 开启)
func IsEngineCryptoEnabled() bool {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("YAO_ENGINE_CRYPTO")))
	if env == "off" || env == "false" || env == "0" || env == "no" {
		return false
	}
	if env == "on" || env == "true" || env == "1" || env == "yes" {
		return true
	}
	// 兼容 CRYPTO_ENABLED 环境变量
	serviceCryptoEnv := strings.ToLower(strings.TrimSpace(os.Getenv("CRYPTO_ENABLED")))
	if serviceCryptoEnv == "off" || serviceCryptoEnv == "false" || serviceCryptoEnv == "0" {
		return false
	}
	return true
}

// GetCryptoConfig 获取当前引擎使用的加解密 Key, IV, Kid
// 严格杜绝任何业务特定项目名称硬编码
func GetCryptoConfig() (key, iv []byte, kid string) {
	k := os.Getenv("LOCAL_CRYPTO_KEY")
	if len(k) != 16 {
		k = "0123456789abcdef" // 16 字节通用默认回退 Key
	}
	v := os.Getenv("LOCAL_CRYPTO_IV")
	if len(v) != 16 {
		v = "fedcba9876543210" // 16 字节通用默认回退 IV
	}
	kd := os.Getenv("LOCAL_CRYPTO_KID")
	if kd == "" {
		kd = "default-sm4-1"
	}
	return []byte(k), []byte(v), kd
}

// LoadEnginePolicyJSON 暴露包级方法加载策略
func LoadEnginePolicyJSON(data []byte) error {
	return globalPolicy.LoadPolicyJSON(data)
}

// LoadEnginePolicyFile 暴露包级方法从文件加载策略
func LoadEnginePolicyFile(file string) error {
	return globalPolicy.LoadPolicyFile(file)
}

// AutoLoadEnginePolicy 暴露包级方法执行自动策略发现加载
func AutoLoadEnginePolicy() error {
	return globalPolicy.AutoLoadPolicy()
}

// RegisterEngineField 暴露包级方法注册字段策略
func RegisterEngineField(rule FieldPolicyRule) {
	globalPolicy.RegisterField(rule)
}

// RegisterEngineWhitelist 暴露包级方法注册白名单前缀
func RegisterEngineWhitelist(prefix string) {
	globalPolicy.RegisterWhitelist(prefix)
}

// RegisterEngineModelTable 暴露包级方法注册模型与物理表对应关系
func RegisterEngineModelTable(modelID, table string) {
	globalPolicy.RegisterModelTable(modelID, table)
}

// ResetEnginePolicy 重置策略缓存（供测试隔离使用）
func ResetEnginePolicy() {
	globalPolicy.Reset()
}

// EncryptMap 对 Map 按照表策略执行加密转换
func EncryptMap(tableOrModel string, row map[string]interface{}) map[string]interface{} {
	if !IsEngineCryptoEnabled() || IsWhiteListed(tableOrModel) || row == nil {
		return row
	}
	key, iv, kid := GetCryptoConfig()
	for col, val := range row {
		if val == nil {
			continue
		}
		rule, has := globalPolicy.GetFieldRule(tableOrModel, col)
		if !has {
			continue
		}
		strVal, isStr := val.(string)
		if !isStr || strVal == "" || IsEnvelope(strVal) {
			continue
		}
		if rule.Op == "encrypt" {
			cipherB64, err := SM4CBCEncrypt([]byte(strVal), key, iv)
			if err == nil {
				row[col] = EncodeEnvelope("SM4/CBC/PKCS7", kid, cipherB64)
			}
		} else if rule.Op == "hash" {
			hashVal := HmacSM3Hex(key, []byte(strVal))
			row[col] = EncodeEnvelope("HMAC-SM3", kid, hashVal)
		}
	}
	return row
}

// DecryptMap 对 Map 包含的密文封套执行解密转换
func DecryptMap(tableOrModel string, row map[string]interface{}) map[string]interface{} {
	if !IsEngineCryptoEnabled() || row == nil {
		return row
	}
	key, iv, _ := GetCryptoConfig()
	for col, val := range row {
		strVal, ok := val.(string)
		if !ok || !IsEnvelope(strVal) {
			continue
		}
		plain, err := DecryptEnvelope(strVal, key, iv)
		if err == nil {
			row[col] = plain
		}
	}
	return row
}

// SniffAndDecrypt 递归嗅探并解密任意数据结构中的国密封套密文
func SniffAndDecrypt(val interface{}) interface{} {
	if !IsEngineCryptoEnabled() || val == nil {
		return val
	}
	key, iv, _ := GetCryptoConfig()

	switch v := val.(type) {
	case string:
		if IsEnvelope(v) {
			plain, err := DecryptEnvelope(v, key, iv)
			if err == nil {
				return plain
			}
		}
		return v

	case maps.MapStrAny:
		for k, item := range v {
			v[k] = SniffAndDecrypt(item)
		}
		return v

	case share.Record:
		for k, item := range v {
			v[k] = SniffAndDecrypt(item)
		}
		return v

	case []maps.MapStrAny:
		for i := range v {
			for k, item := range v[i] {
				v[i][k] = SniffAndDecrypt(item)
			}
		}
		return v

	case []share.Record:
		for i := range v {
			for k, item := range v[i] {
				v[i][k] = SniffAndDecrypt(item)
			}
		}
		return v

	case share.Paginate:
		for i := range v.Items {
			for k, item := range v.Items[i] {
				v.Items[i][k] = SniffAndDecrypt(item)
			}
		}
		return v

	case *share.Paginate:
		if v != nil {
			for i := range v.Items {
				for k, item := range v.Items[i] {
					v.Items[i][k] = SniffAndDecrypt(item)
				}
			}
		}
		return v

	case map[string]interface{}:
		for k, item := range v {
			v[k] = SniffAndDecrypt(item)
		}
		return v

	case []map[string]interface{}:
		for i := range v {
			for k, item := range v[i] {
				v[i][k] = SniffAndDecrypt(item)
			}
		}
		return v

	case []interface{}:
		for i, item := range v {
			v[i] = SniffAndDecrypt(item)
		}
		return v

	default:
		// 通用反射兜底（覆盖 xun/ORM 返回的自定义 map/slice 结构）
		rv := reflect.ValueOf(val)
		if rv.Kind() == reflect.Map {
			for _, key := range rv.MapKeys() {
				elem := rv.MapIndex(key)
				if elem.CanInterface() {
					decrypted := SniffAndDecrypt(elem.Interface())
					rv.SetMapIndex(key, reflect.ValueOf(decrypted))
				}
			}
			return val
		} else if rv.Kind() == reflect.Slice {
			for i := 0; i < rv.Len(); i++ {
				elem := rv.Index(i)
				if elem.CanInterface() {
					decrypted := SniffAndDecrypt(elem.Interface())
					if elem.CanSet() {
						elem.Set(reflect.ValueOf(decrypted))
					}
				}
			}
			return val
		}
		return val
	}
}

// -------------------------------------------------------------
// Process 处理器实现
// -------------------------------------------------------------

func processPolicyLoad(process *process.Process) interface{} {
	process.ValidateArgNums(1)
	arg := process.ArgsString(0)
	if strings.HasPrefix(strings.TrimSpace(arg), "{") {
		globalPolicy.Reset()
		err := globalPolicy.LoadPolicyJSON([]byte(arg))
		if err != nil {
			return map[string]interface{}{"code": 400, "message": err.Error()}
		}
		return map[string]interface{}{"code": 200, "message": "ok"}
	}
	err := globalPolicy.LoadPolicyFile(arg)
	if err != nil {
		return map[string]interface{}{"code": 400, "message": err.Error()}
	}
	return map[string]interface{}{"code": 200, "message": "ok"}
}

func processPolicyReload(process *process.Process) interface{} {
	globalPolicy.Reset()
	err := globalPolicy.AutoLoadPolicy()
	if err != nil {
		return map[string]interface{}{"code": 500, "message": err.Error()}
	}
	return map[string]interface{}{"code": 200, "message": "ok"}
}

func processPolicyGet(process *process.Process) interface{} {
	globalPolicy.ensureLoaded()
	globalPolicy.mu.RLock()
	defer globalPolicy.mu.RUnlock()
	res := make(map[string]interface{})
	for table, fields := range globalPolicy.tableFields {
		cols := make(map[string]interface{})
		for col, r := range fields {
			cols[col] = r
		}
		res[table] = cols
	}
	return res
}
