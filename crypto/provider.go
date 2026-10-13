package crypto

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/gou/connector"
)

// Provider 密码驱动抽象接口
// 支持本地软件驱动 (LocalProvider)、硬件密码机驱动 (KmsProvider) 或透传驱动 (PassThroughProvider)
type Provider interface {
	ID() string
	IsLocal() bool
	Encrypt(plain string, rule FieldPolicyRule) (string, error)
	Decrypt(cipher string) (string, error)
	Hash(plain string, rule FieldPolicyRule) (string, error)
}

// LocalProvider 本地纯内存软件国密驱动 (基于 SM4-CBC / SM4-ECB / HMAC-SM3)
type LocalProvider struct {
	id  string
	key []byte
	iv  []byte
	kid string
}

// PassThroughProvider 透传驱动 (用于 KMS 等外部硬件密码机或未开启本地计算场景)
// 保证 Go 引擎内核切面不使用本地假 Key 误伤远程密码机密文
type PassThroughProvider struct {
	id string
}

// ErrorProvider 记录驱动配置或初始化异常，阻断任何不安全的数据加解密操作
type ErrorProvider struct {
	id  string
	err error
}

// ProviderManager 统一管理当前激活的密码驱动 (并发安全)
type ProviderManager struct {
	mu       sync.RWMutex
	current  Provider
	cachedID string
}

var globalProviders = &ProviderManager{}

// GetActiveProvider 获取当前生效的密码驱动实例
func GetActiveProvider() Provider {
	return globalProviders.Get()
}

// Get 获取当前激活的驱动 (并发安全)
func (m *ProviderManager) Get() Provider {
	providerID := GetCryptoProvider()

	m.mu.RLock()
	if m.current != nil && m.cachedID == providerID {
		defer m.mu.RUnlock()
		return m.current
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != nil && m.cachedID == providerID {
		return m.current
	}

	m.current = m.createProvider(providerID)
	m.cachedID = providerID
	return m.current
}

// Reset 重置驱动缓存 (测试隔离使用)
func (m *ProviderManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = nil
	m.cachedID = ""
}

// createProvider 构造具体驱动实例
func (m *ProviderManager) createProvider(id string) Provider {
	if !IsLocalProvider() {
		return &PassThroughProvider{id: id}
	}
	local, err := NewLocalProvider(id)
	if err != nil {
		return &ErrorProvider{id: id, err: err}
	}
	return local
}

// NewLocalProvider 构造本地软件国密驱动，所有配置严格来源于指定的 Connector (gou/connector)
// Connector DSL (如 connectors/local.conn.yao) 为唯一配置源，若需环境变量可直接在 DSL 中配置 $ENV.XXX (与 redis/database connector 规范一致)
// 严格遵守 Fail-Fast 原则，绝不允许任何硬编码伪密钥兜底！
func NewLocalProvider(id string) (*LocalProvider, error) {
	// 1. 从 Yao 原生 Connector 基础设施 (connector.Select) 获取连接器
	conn, err := connector.Select(id)
	if err != nil && application.App != nil {
		// 若连接器尚未被预加载，尝试通过规范路径按需加载
		connFile := fmt.Sprintf("connectors/%s.conn.yao", id)
		if has, _ := application.App.Exists(connFile); has {
			conn, err = connector.Load(connFile, id)
		}
	}

	if conn == nil || err != nil {
		return nil, fmt.Errorf("crypto provider '%s': connector not found or failed to load: %v", id, err)
	}

	// 2. 从 Connector Setting 提取参数 (DSL 中的 $ENV.xxx 已在 Connector 注册时统一解析)
	var k, v, kd string
	setting := conn.Setting()
	if params, ok := setting["params"].(map[string]interface{}); ok {
		if keyStr, ok := params["key"].(string); ok {
			k = keyStr
		}
		if ivStr, ok := params["iv"].(string); ok {
			v = ivStr
		}
		if kidStr, ok := params["kid"].(string); ok {
			kd = kidStr
		} else if keyCodeStr, ok := params["keyCode"].(string); ok {
			kd = keyCodeStr
		}
	}

	// 3. 严格参数校验 (Fail-Fast: 杜绝硬编码弱密码兜底造成 CWE-798 安全漏洞)
	if len(k) != 16 {
		return nil, fmt.Errorf("crypto provider '%s': invalid or missing key in connector setting, SM4 requires exactly 16 bytes (got %d bytes)", id, len(k))
	}
	if len(v) != 16 {
		return nil, fmt.Errorf("crypto provider '%s': invalid or missing iv in connector setting, SM4 requires exactly 16 bytes (got %d bytes)", id, len(v))
	}
	if kd == "" {
		return nil, fmt.Errorf("crypto provider '%s': missing key identifier (kid or keyCode) in connector setting", id)
	}

	return &LocalProvider{
		id:  id,
		key: []byte(k),
		iv:  []byte(v),
		kid: kd,
	}, nil
}

// -------------------------------------------------------------
// LocalProvider 方法实现
// -------------------------------------------------------------

func (p *LocalProvider) ID() string    { return p.id }
func (p *LocalProvider) IsLocal() bool { return true }
func (p *LocalProvider) Key() []byte   { return p.key }
func (p *LocalProvider) IV() []byte    { return p.iv }
func (p *LocalProvider) Kid() string   { return p.kid }

func (p *LocalProvider) Encrypt(plain string, rule FieldPolicyRule) (string, error) {
	if plain == "" || IsEnvelope(plain) {
		return plain, nil
	}
	cipherB64, err := SM4CBCEncrypt([]byte(plain), p.key, p.iv)
	if err != nil {
		return plain, err
	}
	return EncodeEnvelope("SM4/CBC/PKCS7", p.kid, cipherB64), nil
}

func (p *LocalProvider) Decrypt(cipher string) (string, error) {
	if !IsEnvelope(cipher) {
		return cipher, nil
	}
	return DecryptEnvelope(cipher, p.key, p.iv)
}

func (p *LocalProvider) Hash(plain string, rule FieldPolicyRule) (string, error) {
	if plain == "" || IsEnvelope(plain) {
		return plain, nil
	}
	hashVal := HmacSM3Hex(p.key, []byte(plain))
	return EncodeEnvelope("HMAC-SM3", p.kid, hashVal), nil
}

// -------------------------------------------------------------
// PassThroughProvider 方法实现
// -------------------------------------------------------------

func (p *PassThroughProvider) ID() string                                                 { return p.id }
func (p *PassThroughProvider) IsLocal() bool                                              { return false }
func (p *PassThroughProvider) Encrypt(plain string, rule FieldPolicyRule) (string, error) { return plain, nil }
func (p *PassThroughProvider) Decrypt(cipher string) (string, error)                      { return cipher, nil }
func (p *PassThroughProvider) Hash(plain string, rule FieldPolicyRule) (string, error)   { return plain, nil }

// -------------------------------------------------------------
// ErrorProvider 方法实现
// -------------------------------------------------------------

func (p *ErrorProvider) ID() string                                                 { return p.id }
func (p *ErrorProvider) IsLocal() bool                                              { return false }
func (p *ErrorProvider) Encrypt(plain string, rule FieldPolicyRule) (string, error) { return plain, p.err }
func (p *ErrorProvider) Decrypt(cipher string) (string, error)                      { return cipher, p.err }
func (p *ErrorProvider) Hash(plain string, rule FieldPolicyRule) (string, error)   { return plain, p.err }
func (p *ErrorProvider) Error() error                                              { return p.err }

// -------------------------------------------------------------
// 密码配置与提供者环境查询
// -------------------------------------------------------------

// GetCryptoProvider 获取当前生效的密码提供者驱动名称 (默认: local)
func GetCryptoProvider() string {
	p := strings.ToLower(strings.TrimSpace(os.Getenv("CRYPTO_PROVIDER")))
	if p == "" {
		return "local"
	}
	return p
}

// IsLocalProvider 判断当前驱动是否为本地软件国密驱动
func IsLocalProvider() bool {
	p := GetCryptoProvider()
	if p == "kms" || strings.Contains(p, "kms") || strings.Contains(p, "hsm") {
		return false
	}
	return true
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
// 严格杜绝任何虚假伪密钥兜底；若未正确配置或非 LocalProvider，返回 nil, nil, ""
func GetCryptoConfig() (key, iv []byte, kid string) {
	provider := GetActiveProvider()
	if local, ok := provider.(*LocalProvider); ok {
		return local.Key(), local.IV(), local.Kid()
	}
	return nil, nil, ""
}

// ResetProviderManager 重置驱动管理器缓存 (供测试使用)
func ResetProviderManager() {
	globalProviders.Reset()
}
