package crypto

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/connector"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/gou/query/share"
	"github.com/yaoapp/kun/maps"
)

func TestPolicyManagerDynamicLoad(t *testing.T) {
	ResetEnginePolicy()
	defer ResetEnginePolicy()

	policyJSON := []byte(`{
		"policyVersion": 1,
		"tables": [
			{"name": "test_users", "model": "user", "action": "protect"}
		],
		"fields": [
			{"table": "test_users", "column": "mobile", "op": "encrypt", "alg": "SM4/CBC/PKCS7"},
			{"table": "test_users", "column": "id_card", "op": "encrypt", "alg": "SM4/CBC/PKCS7"},
			{"table": "test_users", "column": "secret", "op": "hash", "alg": "HMAC-SM3"}
		]
	}`)

	err := LoadEnginePolicyJSON(policyJSON)
	assert.Nil(t, err)

	// 验证动态加载的策略规则
	rule, found := globalPolicy.GetFieldRule("user", "mobile")
	assert.True(t, found)
	assert.Equal(t, "encrypt", rule.Op)
	assert.Equal(t, "SM4/CBC/PKCS7", rule.Alg)

	ruleID, foundID := globalPolicy.GetFieldRule("test_users", "id_card")
	assert.True(t, foundID)
	assert.Equal(t, "encrypt", ruleID.Op)

	// 验证未配置的字段
	_, notFound := globalPolicy.GetFieldRule("test_users", "nickname")
	assert.False(t, notFound)
}

func TestPolicyAutoLoadFromFile(t *testing.T) {
	ResetEnginePolicy()
	defer ResetEnginePolicy()

	tmpDir := t.TempDir()
	policyFile := filepath.Join(tmpDir, "test_field_policy.json")

	content := []byte(`{
		"policyVersion": "v1.0",
		"fields": [
			{"table": "auto_load_table", "column": "card_number", "op": "encrypt", "alg": "SM4/CBC/PKCS7"}
		]
	}`)
	err := os.WriteFile(policyFile, content, 0644)
	assert.Nil(t, err)

	os.Setenv("YAO_CRYPTO_POLICY_FILE", policyFile)
	defer os.Unsetenv("YAO_CRYPTO_POLICY_FILE")

	err = AutoLoadEnginePolicy()
	assert.Nil(t, err)

	rule, found := globalPolicy.GetFieldRule("auto_load_table", "card_number")
	assert.True(t, found)
	assert.Equal(t, "encrypt", rule.Op)
}

func TestPolicyProcesses(t *testing.T) {
	ResetEnginePolicy()
	defer ResetEnginePolicy()

	// 1. 测试 crypto.policy.load 通过 JSON 字符串载入
	jsonStr := `{"fields": [{"table": "api_table", "column": "api_secret", "op": "encrypt", "alg": "SM4/CBC/PKCS7"}]}`
	pLoad := process.New("crypto.policy.load", jsonStr)
	resLoad, err := pLoad.Exec()
	assert.Nil(t, err)
	assert.Equal(t, 200, resLoad.(map[string]interface{})["code"])

	// 2. 测试 crypto.policy.get 查询当前所有加载的策略
	pGet := process.New("crypto.policy.get")
	resGet, err := pGet.Exec()
	assert.Nil(t, err)
	allPolicies := resGet.(map[string]interface{})
	tableMap, ok := allPolicies["api_table"].(map[string]interface{})
	assert.True(t, ok)
	assert.NotNil(t, tableMap["api_secret"])
}

func TestPolicyWhitelistConfiguration(t *testing.T) {
	ResetEnginePolicy()
	defer ResetEnginePolicy()

	policyJSON := []byte(`{
		"policyVersion": 1,
		"whitelist": [
			"custom_log_",
			"audit_sys",
			"sys_crypto"
		],
		"fields": [
			{"table": "custom_log_access", "column": "secret_token", "op": "encrypt", "alg": "SM4/CBC/PKCS7"},
			{"table": "normal_biz", "column": "mobile", "op": "encrypt", "alg": "SM4/CBC/PKCS7"}
		]
	}`)

	err := LoadEnginePolicyJSON(policyJSON)
	assert.Nil(t, err)

	// 1. 白名单命中判断
	assert.True(t, IsWhiteListed("custom_log_access"), "配置的 custom_log_ 前缀必须命中白名单")
	assert.True(t, IsWhiteListed("custom_log_20261012"), "前缀匹配生效")
	assert.True(t, IsWhiteListed("audit_sys_detail"), "配置的 audit_sys 前缀必须命中白名单")
	assert.True(t, IsWhiteListed("models.sys.user"), "引擎内部 models. 必须兜底命中白名单")
	assert.False(t, IsWhiteListed("normal_biz"), "未在白名单的业务表返回 false")

	// 2. 验证 EncryptMap 对白名单表的完全穿透保护
	row := map[string]interface{}{
		"secret_token": "my-plain-token-12345",
	}
	resRow := EncryptMap("custom_log_access", row)
	assert.Equal(t, "my-plain-token-12345", resRow["secret_token"], "白名单表必须直接穿透，绝不执行加密")
}

// TestProviderConnectorIntegration 测试通过 gou/connector 原生基础设施装配密码驱动
func TestProviderConnectorIntegration(t *testing.T) {
	ResetProviderManager()
	defer ResetProviderManager()

	// 1. 注册一个标准 config 类型 Connector
	connDSL := []byte(`{
		"name": "Custom Test Connector",
		"type": "config",
		"options": {
			"params": {
				"key": "testkey123456789",
				"iv":  "testiv9876543210",
				"kid": "custom-connector-kid"
			}
		}
	}`)
	_, err := connector.New("config", "test_crypto_conn", connDSL)
	assert.Nil(t, err)
	defer connector.Remove("test_crypto_conn")

	// 2. 设置当前驱动指向该 connector
	os.Setenv("CRYPTO_PROVIDER", "test_crypto_conn")
	defer os.Unsetenv("CRYPTO_PROVIDER")

	ResetProviderManager()

	provider := GetActiveProvider()
	assert.Equal(t, "test_crypto_conn", provider.ID())
	assert.True(t, provider.IsLocal())

	local, ok := provider.(*LocalProvider)
	assert.True(t, ok)
	assert.Equal(t, []byte("testkey123456789"), local.Key())
	assert.Equal(t, []byte("testiv9876543210"), local.IV())
	assert.Equal(t, "custom-connector-kid", local.Kid())

	// 3. 验证基于该 Connector 的加解密闭环
	rule := FieldPolicyRule{Op: "encrypt", Alg: "SM4/CBC/PKCS7"}
	cipher, err := provider.Encrypt("13800138000", rule)
	assert.Nil(t, err)
	assert.True(t, IsEnvelope(cipher))
	assert.Contains(t, cipher, "custom-connector-kid")

	plain, err := provider.Decrypt(cipher)
	assert.Nil(t, err)
	assert.Equal(t, "13800138000", plain)
}

// TestPassThroughProviderKms 验证 KMS 硬件模式下 Go 引擎切面的安全透传
func TestPassThroughProviderKms(t *testing.T) {
	ResetProviderManager()
	defer ResetProviderManager()

	os.Setenv("CRYPTO_PROVIDER", "kms")
	defer os.Unsetenv("CRYPTO_PROVIDER")

	ResetProviderManager()

	provider := GetActiveProvider()
	assert.Equal(t, "kms", provider.ID())
	assert.False(t, provider.IsLocal(), "kms 模式非本地软解密")

	rule := FieldPolicyRule{Op: "encrypt", Alg: "SM4/CBC/PKCS7"}
	enc, err := provider.Encrypt("hello-kms", rule)
	assert.Nil(t, err)
	assert.Equal(t, "hello-kms", enc, "KMS 模式下引擎切面不进行本地假 Key 加密")

	dec, err := provider.Decrypt("ENC:SM4/CBC/PKCS7:remote-kid:xxxx")
	assert.Nil(t, err)
	assert.Equal(t, "ENC:SM4/CBC/PKCS7:remote-kid:xxxx", dec, "KMS 模式下引擎切面安全透传远程密文")
}

// TestSniffAndDecryptRecursive 验证递归数据结构嗅探解密
func TestSniffAndDecryptRecursive(t *testing.T) {
	ResetProviderManager()
	defer ResetProviderManager()
	ResetEnginePolicy()
	defer ResetEnginePolicy()

	// 注册 local 连接器供测试使用
	connDSL := []byte(`{
		"name": "Local Test Connector",
		"type": "config",
		"options": {
			"params": {
				"key": "1234567890123456",
				"iv":  "6543210987654321",
				"kid": "test-kid"
			}
		}
	}`)
	_, _ = connector.New("config", "local", connDSL)
	defer connector.Remove("local")

	os.Setenv("CRYPTO_PROVIDER", "local")
	defer os.Unsetenv("CRYPTO_PROVIDER")

	local := GetActiveProvider()
	assert.True(t, local.IsLocal())
	rule := FieldPolicyRule{Op: "encrypt", Alg: "SM4/CBC/PKCS7"}
	cipher, _ := local.Encrypt("plain_mobile_138", rule)

	// 测试复杂嵌套结构嗅探解密 (包含 MapStrAny, share.Record, slice)
	data := maps.MapStrAny{
		"user": map[string]interface{}{
			"mobile": cipher,
		},
		"records": []share.Record{
			{"contact": cipher},
		},
	}

	decrypted := SniffAndDecrypt(data).(maps.MapStrAny)
	userMap := decrypted["user"].(map[string]interface{})
	assert.Equal(t, "plain_mobile_138", userMap["mobile"])

	recordsList := decrypted["records"].([]share.Record)
	assert.Equal(t, "plain_mobile_138", recordsList[0]["contact"])
}

// TestConnectorEnvVarResolution 验证 Connector DSL 中使用 $ENV.xxx 声明环境变量的统一解析
func TestConnectorEnvVarResolution(t *testing.T) {
	ResetProviderManager()
	defer ResetProviderManager()

	// 设置系统环境变量
	os.Setenv("MY_CRYPTO_KEY", "envkey_123456789")
	os.Setenv("MY_CRYPTO_IV", "enviv__987654321")
	os.Setenv("MY_CRYPTO_KID", "env-resolved-kid")
	defer os.Unsetenv("MY_CRYPTO_KEY")
	defer os.Unsetenv("MY_CRYPTO_IV")
	defer os.Unsetenv("MY_CRYPTO_KID")

	// Connector DSL 像 redis.conn.yao 一样直接声明 $ENV.xxx
	connDSL := []byte(`{
		"name": "Env Driven Connector",
		"type": "config",
		"options": {
			"params": {
				"key": "$ENV.MY_CRYPTO_KEY",
				"iv":  "$ENV.MY_CRYPTO_IV",
				"kid": "$ENV.MY_CRYPTO_KID"
			}
		}
	}`)
	_, err := connector.New("config", "env_crypto_conn", connDSL)
	assert.Nil(t, err)
	defer connector.Remove("env_crypto_conn")

	// 驱动从 Connector Setting 读取，自动获取由 Connector 解析好的环境变量值
	prov, err := NewLocalProvider("env_crypto_conn")
	assert.Nil(t, err)
	assert.Equal(t, []byte("envkey_123456789"), prov.Key())
	assert.Equal(t, []byte("enviv__987654321"), prov.IV())
	assert.Equal(t, "env-resolved-kid", prov.Kid())
}

// TestLocalProviderFailFastOnMissingConfig 严苛验证：缺少连接器或配置不合法时必须 Fail-Fast 报错，杜绝任何弱密码兜底
func TestLocalProviderFailFastOnMissingConfig(t *testing.T) {
	ResetProviderManager()
	defer ResetProviderManager()

	os.Setenv("CRYPTO_PROVIDER", "non_existent_provider_no_fallback")
	defer os.Unsetenv("CRYPTO_PROVIDER")

	// 1. 尝试使用不存在的连接器构造 LocalProvider 必须明确返回错误
	prov, err := NewLocalProvider("non_existent_provider_no_fallback")
	assert.Nil(t, prov, "缺失 Connector 时不得构造成功")
	assert.NotNil(t, err, "必须明确报错，阻止非法启动")
	assert.Contains(t, err.Error(), "connector not found")

	// 2. 验证活跃驱动转入 ErrorProvider，阻断任何加密操作
	active := GetActiveProvider()
	errProv, ok := active.(*ErrorProvider)
	assert.True(t, ok, "缺失配置必须转为 ErrorProvider")
	assert.NotNil(t, errProv.Error())

	// 3. 验证此时调用 Encrypt 绝不使用假 Key 写入脏数据，而是直接返回配置错误
	rule := FieldPolicyRule{Op: "encrypt", Alg: "SM4/CBC/PKCS7"}
	_, encErr := active.Encrypt("sensitive-data", rule)
	assert.NotNil(t, encErr, "未配密钥时加密必须被安全阻断")
}

