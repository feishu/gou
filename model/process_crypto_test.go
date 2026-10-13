package model

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/connector"
	"github.com/yaoapp/gou/crypto"
	"github.com/yaoapp/gou/process"
	kunAny "github.com/yaoapp/kun/any"
	"github.com/yaoapp/kun/maps"
	"github.com/yaoapp/xun/capsule"
)

// TestModelCryptoHooks_ABTesting 全面验证 Model 级切面钩子在态 A（关闭态）与态 B（开启态）下的 A/B 对比
func TestModelCryptoHooks_ABTesting(t *testing.T) {
	oldEngineCrypto := os.Getenv("YAO_ENGINE_CRYPTO")
	defer os.Setenv("YAO_ENGINE_CRYPTO", oldEngineCrypto)

	crypto.ResetEnginePolicy()
	defer crypto.ResetEnginePolicy()

	// 显式注册测试策略（杜绝底座任何硬编码依赖）
	crypto.RegisterEngineModelTable("sys.patient", "sys_patients")
	crypto.RegisterEngineModelTable("sys.user_auths", "sys_user_auths")
	crypto.RegisterEngineField(crypto.FieldPolicyRule{Table: "sys_patients", Column: "patient_mobile", Op: "encrypt", Alg: "SM4/CBC/PKCS7"})
	crypto.RegisterEngineField(crypto.FieldPolicyRule{Table: "sys_patients", Column: "patient_card_no", Op: "encrypt", Alg: "SM4/CBC/PKCS7"})
	crypto.RegisterEngineField(crypto.FieldPolicyRule{Table: "sys_patients", Column: "patient_name", Op: "encrypt", Alg: "SM4/CBC/PKCS7"})
	crypto.RegisterEngineField(crypto.FieldPolicyRule{Table: "sys_user_auths", Column: "password", Op: "hash", Alg: "HMAC-SM3"})
	crypto.RegisterEngineWhitelist("sys_crypto")

	// 规范注册 local 测试连接器 (配置驱动，绝不依赖底座代码硬编码兜底)
	connDSL := []byte(`{
		"name": "Local Test Connector",
		"type": "config",
		"options": {
			"params": {
				"key": "0123456789abcdef",
				"iv":  "fedcba9876543210",
				"kid": "test-sm4-1"
			}
		}
	}`)
	_, _ = connector.New("config", "local", connDSL)
	defer connector.Remove("local")
	crypto.ResetProviderManager()
	defer crypto.ResetProviderManager()

	key, iv, _ := crypto.GetCryptoConfig()

	// ==========================================
	// 态 A: YAO_ENGINE_CRYPTO=off (对照组：纯透传，零加密，零解密)
	// ==========================================
	os.Setenv("YAO_ENGINE_CRYPTO", "off")
	assert.False(t, crypto.IsEngineCryptoEnabled(), "态 A: 引擎国密开关必须处于关闭状态")

	// 1. 态 A 写路径 (beforePersistEncrypt)
	rowA := map[string]interface{}{
		"patient_mobile":  "13800138000",
		"patient_card_no": "520101199001011234",
		"patient_name":    "张三丰",
		"status":          1,
	}
	resRowA := beforePersistEncrypt("sys.patient", rowA)
	assert.Equal(t, "13800138000", resRowA["patient_mobile"], "态 A: 手机号必须保持原始明文")
	assert.Equal(t, "520101199001011234", resRowA["patient_card_no"], "态 A: 身份证必须保持原始明文")
	assert.Equal(t, "张三丰", resRowA["patient_name"], "态 A: 姓名必须保持原始明文")
	assert.False(t, crypto.IsEnvelope(resRowA["patient_mobile"].(string)), "态 A: 绝不能有信封包装")

	// 2. 态 A 二维批量写路径 (beforeInsertEncrypt)
	colsA := []string{"id", "patient_mobile", "patient_name"}
	rowsA := [][]interface{}{
		{1, "13800138000", "张三丰"},
		{2, "13900139000", "李四"},
	}
	_, resRowsA := beforeInsertEncrypt("sys.patient", colsA, rowsA)
	assert.Equal(t, "13800138000", resRowsA[0][1], "态 A: 二维批量第一行手机号保持明文")
	assert.Equal(t, "13900139000", resRowsA[1][1], "态 A: 二维批量第二行手机号保持明文")

	// 3. 态 A 读路径 (afterQueryDecrypt)
	cipherMobile, _ := crypto.SM4CBCEncrypt([]byte("13800138000"), key, iv)
	envMobile := crypto.EncodeEnvelope("SM4/CBC/PKCS7", "local-sm4-1", cipherMobile)
	queryDataA := maps.MapStrAny{
		"id":             1,
		"patient_mobile": envMobile,
	}
	resQueryA := afterQueryDecrypt("sys.patient", queryDataA).(maps.MapStrAny)
	assert.Equal(t, envMobile, resQueryA["patient_mobile"], "态 A: 密文保持原样信封，绝不执行解密")

	// ==========================================
	// 态 B: YAO_ENGINE_CRYPTO=on (实验组：透明自动加密与解密)
	// ==========================================
	os.Setenv("YAO_ENGINE_CRYPTO", "on")
	assert.True(t, crypto.IsEngineCryptoEnabled(), "态 B: 引擎国密开关必须处于开启状态")

	// 1. 态 B 写路径 (beforePersistEncrypt) 涉密列自动加密，非涉密列保持不变
	rowB := map[string]interface{}{
		"patient_mobile":  "13800138000",
		"patient_card_no": "520101199001011234",
		"patient_name":    "张三丰",
		"status":          1,
		"age":             45,
	}
	resRowB := beforePersistEncrypt("sys.patient", rowB)
	assert.True(t, crypto.IsEnvelope(resRowB["patient_mobile"].(string)), "态 B: 涉密手机号必须自动加密为国密信封")
	assert.True(t, crypto.IsEnvelope(resRowB["patient_card_no"].(string)), "态 B: 涉密身份证必须自动加密为国密信封")
	assert.True(t, crypto.IsEnvelope(resRowB["patient_name"].(string)), "态 B: 涉密姓名必须自动加密为国密信封")
	assert.True(t, strings.HasPrefix(resRowB["patient_mobile"].(string), "v1.SM4"), "态 B: 必须符合 v1.SM4 封套格式")
	assert.Equal(t, 1, resRowB["status"], "态 B: 非涉密数字列保持原样")
	assert.Equal(t, 45, resRowB["age"], "态 B: 非涉密数字列保持原样")

	// 验证加密结果可正确解密出原始明文
	plainMobile, err := crypto.DecryptEnvelope(resRowB["patient_mobile"].(string), key, iv)
	assert.Nil(t, err)
	assert.Equal(t, "13800138000", plainMobile, "态 B: 加密信封解密后与原始明文完全一致")

	// 2. 态 B 哈希策略测试 (sys.user_auths 密码字段自动 HMAC-SM3)
	authRow := map[string]interface{}{
		"id":       1001,
		"password": "PasswordSecret999",
	}
	resAuthRow := beforePersistEncrypt("sys.user_auths", authRow)
	assert.True(t, crypto.IsEnvelope(resAuthRow["password"].(string)), "态 B: 密码字段必须自动 HMAC-SM3 哈希")
	assert.True(t, strings.HasPrefix(resAuthRow["password"].(string), "v1.HMAC-SM3."), "态 B: 必须符合 v1.HMAC-SM3 封套格式")

	// 3. 态 B 二维批量写路径 (beforeInsertEncrypt)
	colsB := []string{"id", "patient_mobile", "status", "patient_name"}
	rowsB := [][]interface{}{
		{1, "13800138000", 1, "张三丰"},
		{2, "13900139000", 0, "李四"},
	}
	_, resRowsB := beforeInsertEncrypt("sys.patient", colsB, rowsB)
	assert.True(t, crypto.IsEnvelope(resRowsB[0][1].(string)), "态 B: 第1行手机号自动加密")
	assert.True(t, crypto.IsEnvelope(resRowsB[0][3].(string)), "态 B: 第1行姓名自动加密")
	assert.Equal(t, 1, resRowsB[0][2], "态 B: 第1行状态非涉密列保持不变")
	assert.True(t, crypto.IsEnvelope(resRowsB[1][1].(string)), "态 B: 第2行手机号自动加密")
	assert.True(t, crypto.IsEnvelope(resRowsB[1][3].(string)), "态 B: 第2行姓名自动加密")

	// 4. 态 B 读路径 (afterQueryDecrypt) 自动嗅探解密
	queryDataB := maps.MapStrAny{
		"id":             1,
		"patient_mobile": resRowB["patient_mobile"],
		"patient_name":   resRowB["patient_name"],
		"status":         1,
	}
	resQueryB := afterQueryDecrypt("sys.patient", queryDataB).(maps.MapStrAny)
	assert.Equal(t, "13800138000", resQueryB["patient_mobile"], "态 B: 手机号自动透明解密为真实手机号")
	assert.Equal(t, "张三丰", resQueryB["patient_name"], "态 B: 姓名自动透明解密为真实姓名")
	assert.Equal(t, 1, resQueryB["status"], "态 B: 非涉密字段保持原样")

	// 5. 态 B 切片批量读路径 ([]maps.MapStrAny 与 []interface{})
	sliceDataB := []maps.MapStrAny{
		{"id": 1, "patient_mobile": resRowB["patient_mobile"]},
		{"id": 2, "patient_mobile": "13900139000(纯明文)"},
	}
	resSliceB := afterQueryDecrypt("sys.patient", sliceDataB).([]maps.MapStrAny)
	assert.Equal(t, "13800138000", resSliceB[0]["patient_mobile"], "态 B: 批量第1项密文解密为明文")
	assert.Equal(t, "13900139000(纯明文)", resSliceB[1]["patient_mobile"], "态 B: 批量第2项明文 100% 幂等保持不变")

	// 6. 白名单防死锁与防递归保护
	whiteListRow := map[string]interface{}{
		"patient_mobile": "13800138000",
		"key":            "my-secret-key",
	}
	resWhite := beforePersistEncrypt("sys_crypto_key", whiteListRow)
	assert.Equal(t, "13800138000", resWhite["patient_mobile"], "白名单表直接穿透，绝不加密")
	resWhiteModels := beforePersistEncrypt("models.sys.user", whiteListRow)
	assert.Equal(t, "13800138000", resWhiteModels["patient_mobile"], "models.* 内部表直接穿透，绝不加密")

	// 7. 幂等性测试（防止双重加密与二次损坏）
	alreadyEncrypted := map[string]interface{}{
		"patient_mobile": resRowB["patient_mobile"], // 已经是 v1.SM4.
	}
	resTwice := beforePersistEncrypt("sys.patient", alreadyEncrypted)
	assert.Equal(t, resRowB["patient_mobile"], resTwice["patient_mobile"], "对已有信封的数据再次加密必须原样返回，杜绝嵌套加密")
}

// TestModelProcess_IntegrationWithDB 全流程验证 Yao Model Process 原子操作在真实数据库写入密文、读出明文
func TestModelProcess_IntegrationWithDB(t *testing.T) {
	oldEngineCrypto := os.Getenv("YAO_ENGINE_CRYPTO")
	defer os.Setenv("YAO_ENGINE_CRYPTO", oldEngineCrypto)

	crypto.ResetEnginePolicy()
	defer crypto.ResetEnginePolicy()

	// 注册测试策略：针对物理表 sys_patients 声明涉密字段
	crypto.RegisterEngineField(crypto.FieldPolicyRule{Table: "sys_patients", Column: "patient_mobile", Op: "encrypt", Alg: "SM4/CBC/PKCS7"})
	crypto.RegisterEngineField(crypto.FieldPolicyRule{Table: "sys_patients", Column: "patient_card_no", Op: "encrypt", Alg: "SM4/CBC/PKCS7"})
	crypto.RegisterEngineField(crypto.FieldPolicyRule{Table: "sys_patients", Column: "patient_name", Op: "encrypt", Alg: "SM4/CBC/PKCS7"})

	testDBPath := "./test_model_process_crypto.db"
	_ = os.Remove(testDBPath)

	if capsule.Global != nil {
		capsule.Global.Connections.Range(func(key, value any) bool {
			if conn, ok := value.(*capsule.Connection); ok {
				conn.Close()
			}
			return true
		})
		capsule.Global = nil
	}

	capsule.AddConn("primary", "sqlite3", testDBPath).SetAsGlobal()
	defer func() {
		if capsule.Global != nil {
			capsule.Global.Connections.Range(func(key, value any) bool {
				if conn, ok := value.(*capsule.Connection); ok {
					conn.Close()
				}
				return true
			})
			capsule.Global = nil
		}
		_ = os.Remove(testDBPath)
	}()

	source := []byte(`{
		"name": "SysPatient",
		"table": { "name": "sys_patients", "comment": "就诊人表" },
		"columns": [
			{ "label": "ID", "name": "id", "type": "ID" },
			{ "label": "姓名", "name": "patient_name", "type": "string", "length": 100, "nullable": true },
			{ "label": "手机号", "name": "patient_mobile", "type": "string", "length": 255, "nullable": true },
			{ "label": "身份证", "name": "patient_card_no", "type": "string", "length": 255, "nullable": true },
			{ "label": "状态", "name": "status", "type": "integer", "nullable": true }
		],
		"option": { "timestamps": false, "soft_deletes": false }
	}`)

	mod, err := LoadSource(source, "sys.patient", "")
	assert.Nil(t, err)
	assert.NotNil(t, mod)
	Models["sys.patient"] = mod

	err = mod.Migrate(true)
	assert.Nil(t, err)

	// -------------------------------------------------------------
	// 态 A: YAO_ENGINE_CRYPTO=off (对照组：裸存明文，裸读明文)
	// -------------------------------------------------------------
	os.Setenv("YAO_ENGINE_CRYPTO", "off")

	pSaveA := process.New("models.sys.patient.save", map[string]interface{}{
		"patient_name":    "明文张三",
		"patient_mobile":  "13800138000",
		"patient_card_no": "520101199001011234",
		"status":          1,
	})
	resSaveA, err := pSaveA.Exec()
	assert.Nil(t, err)
	idA := resSaveA

	// 直接从底层物理表查询（绕过 Model 切面）
	rawRowA, err := capsule.Query().Table("sys_patients").Where("id", idA).First()
	assert.Nil(t, err)
	assert.Equal(t, "13800138000", rawRowA.Get("patient_mobile"), "态 A 物理存储: 保持明文")
	assert.False(t, crypto.IsEnvelope(rawRowA.Get("patient_mobile").(string)), "态 A 物理存储: 无密文信封")

	// 通过 Model Process 查询
	pFindA := process.New("models.sys.patient.find", idA, QueryParam{})
	resFindA, err := pFindA.Exec()
	assert.Nil(t, err)
	rowFindA := resFindA.(maps.MapStrAny)
	assert.Equal(t, "13800138000", rowFindA["patient_mobile"], "态 A 模型读出: 明文手机号")

	// -------------------------------------------------------------
	// 态 B: YAO_ENGINE_CRYPTO=on (实验组：写操作自动加密落库，读操作自动解密出明文)
	// -------------------------------------------------------------
	os.Setenv("YAO_ENGINE_CRYPTO", "on")

	pSaveB := process.New("models.sys.patient.save", map[string]interface{}{
		"patient_name":    "国密李四",
		"patient_mobile":  "13900139000",
		"patient_card_no": "520101199101012345",
		"status":          2,
	})
	resSaveB, err := pSaveB.Exec()
	assert.Nil(t, err)
	idB := resSaveB

	// 1. 物理安全检查：直接从底层数据库表查询真实存储
	rawRowB, err := capsule.Query().Table("sys_patients").Where("id", idB).First()
	assert.Nil(t, err)
	rawMobileB := rawRowB.Get("patient_mobile").(string)
	rawCardB := rawRowB.Get("patient_card_no").(string)
	rawNameB := rawRowB.Get("patient_name").(string)

	assert.True(t, crypto.IsEnvelope(rawMobileB), "态 B 物理存储必须是国密信封")
	assert.True(t, crypto.IsEnvelope(rawCardB), "态 B 物理存储身份证必须是国密信封")
	assert.True(t, crypto.IsEnvelope(rawNameB), "态 B 物理存储姓名必须是国密信封")
	assert.True(t, strings.HasPrefix(rawMobileB, "v1.SM4"), "态 B 物理存储前缀符合规范")

	// 2. 透明解密检查：通过 models.sys.patient.find 读取
	pFindB := process.New("models.sys.patient.find", idB, QueryParam{})
	resFindB, err := pFindB.Exec()
	assert.Nil(t, err)
	rowFindB := resFindB.(maps.MapStrAny)
	assert.Equal(t, "13900139000", rowFindB["patient_mobile"], "态 B 模型 Find: 自动透明解密为真实手机号")
	assert.Equal(t, "520101199101012345", rowFindB["patient_card_no"], "态 B 模型 Find: 自动透明解密为真实身份证")
	assert.Equal(t, "国密李四", rowFindB["patient_name"], "态 B 模型 Find: 自动透明解密为真实姓名")
	assert.False(t, crypto.IsEnvelope(rowFindB["patient_mobile"].(string)), "态 B 上层拿到的数据绝不带密文封套")

	// 3. 批量查询检查：通过 models.sys.patient.get 读取
	pGetB := process.New("models.sys.patient.get", QueryParam{
		Wheres: []QueryWhere{{Column: "id", Value: idB}},
	})
	resGetB, err := pGetB.Exec()
	assert.Nil(t, err)
	rowsGetB := resGetB.([]maps.MapStrAny)
	assert.Equal(t, 1, len(rowsGetB))
	assert.Equal(t, "13900139000", rowsGetB[0]["patient_mobile"], "态 B 模型 Get 列表: 涉密字段自动透明解密")

	// 4. 分页查询检查：通过 models.sys.patient.paginate 读取
	pPaginateB := process.New("models.sys.patient.paginate", QueryParam{}, 1, 10)
	resPaginateB, err := pPaginateB.Exec()
	assert.Nil(t, err)
	pageResB := resPaginateB.(maps.MapStrAny)
	pageDataB := pageResB["data"].([]maps.MapStrAny)
	assert.True(t, len(pageDataB) >= 2)
	for _, item := range pageDataB {
		if item["id"] == idB || kunAny.Of(item["id"]).CInt() == kunAny.Of(idB).CInt() {
			assert.Equal(t, "13900139000", item["patient_mobile"], "态 B 模型 Paginate 分页: 涉密列自动透明解密")
			assert.Equal(t, "国密李四", item["patient_name"], "态 B 模型 Paginate 分页: 姓名自动透明解密")
		}
	}
}
