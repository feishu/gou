package crypto

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/process"
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

