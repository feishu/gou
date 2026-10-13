package query

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/connector"
	"github.com/yaoapp/gou/crypto"
	"github.com/yaoapp/gou/query"
	"github.com/yaoapp/gou/query/gou"
	"github.com/yaoapp/gou/runtime/v8/bridge"
	"github.com/yaoapp/xun/capsule"
	"github.com/yaoapp/xun/dbal/schema"
	"rogchap.com/v8go"
)

func initCryptoTestDB(t *testing.T) {
	testDBPath := "./test_query_crypto.db"
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

	sch := capsule.Schema()
	sch.MustDropTableIfExists("query_crypto_test")
	sch.MustCreateTable("query_crypto_test", func(table schema.Blueprint) {
		table.ID("id")
		table.String("patient_name", 100).Null()
		table.String("patient_mobile", 200).Null()
		table.String("patient_card_no", 200).Null()
		table.Integer("age").Null()
	})

	key, iv, kid := crypto.GetCryptoConfig()
	mobileEnc, _ := crypto.SM4CBCEncrypt([]byte("13800138000"), key, iv)
	cardEnc, _ := crypto.SM4CBCEncrypt([]byte("520101199001011234"), key, iv)
	nameEnc, _ := crypto.SM4CBCEncrypt([]byte("张三丰"), key, iv)

	envMobile := crypto.EncodeEnvelope("SM4/CBC/PKCS7", kid, mobileEnc)
	envCard := crypto.EncodeEnvelope("SM4/CBC/PKCS7", kid, cardEnc)
	envName := crypto.EncodeEnvelope("SM4/CBC/PKCS7", kid, nameEnc)

	qb := capsule.Query()
	qb.Table("query_crypto_test").MustInsert([][]interface{}{
		{1, envName, envMobile, envCard, 45},
		{2, "李四(明文)", "13900139000", "520101199202022345", 30},
	}, []string{"id", "patient_name", "patient_mobile", "patient_card_no", "age"})

	query.Register("query-crypto-test", &gou.Query{
		Query: capsule.Query(),
		GetTableName: func(s string) string {
			return s
		},
	})
}

// TestQueryCrypto_ABTesting 验证 Query 切面在态 A（关闭态）与态 B（开启态）下的 A/B 对比
func TestQueryCrypto_ABTesting(t *testing.T) {
	oldEngineCrypto := os.Getenv("YAO_ENGINE_CRYPTO")
	defer os.Setenv("YAO_ENGINE_CRYPTO", oldEngineCrypto)

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

	initCryptoTestDB(t)
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
		_ = os.Remove("./test_query_crypto.db")
	}()

	iso := v8go.NewIsolate()
	defer iso.Dispose()

	queryObj := &Object{}
	global := v8go.NewObjectTemplate(iso)
	global.Set("Query", queryObj.ExportFunction(iso))

	ctx := v8go.NewContext(iso, global)
	defer ctx.Close()

	// ==========================================
	// 态 A: YAO_ENGINE_CRYPTO=off (对照组：原生透传，零加解密损耗)
	// ==========================================
	os.Setenv("YAO_ENGINE_CRYPTO", "off")
	assert.False(t, crypto.IsEngineCryptoEnabled(), "态 A: 引擎国密开关必须处于关闭状态")

	// 态 A - Get 查询
	vA, err := ctx.RunScript(`
		function GetStateA() {
			var q = new Query("query-crypto-test");
			return q.Get({
				"select": ["id", "patient_name", "patient_mobile", "patient_card_no", "age"],
				"from": "query_crypto_test",
				"order": ["id"]
			});
		}
		GetStateA();
	`, "")
	assert.Nil(t, err)
	resA, err := bridge.GoValue(vA, ctx)
	assert.Nil(t, err)

	rowsA, ok := resA.([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 2, len(rowsA))
	rowA1 := rowsA[0].(map[string]interface{})
	// 态 A 断言：密文保持原样 v1.SM4. 信封，绝不解密
	assert.True(t, crypto.IsEnvelope(rowA1["patient_mobile"].(string)), "态 A: 密文必须原样透传保留信封")
	assert.True(t, crypto.IsEnvelope(rowA1["patient_card_no"].(string)), "态 A: 身份证密文必须原样透传保留信封")
	assert.True(t, crypto.IsEnvelope(rowA1["patient_name"].(string)), "态 A: 姓名密文必须原样透传保留信封")

	// 态 A - First 查询
	vAFirst, err := ctx.RunScript(`
		function FirstStateA() {
			var q = new Query("query-crypto-test");
			return q.First({
				"select": ["patient_mobile"],
				"from": "query_crypto_test",
				"where": [{"field": "id", "value": 1}]
			});
		}
		FirstStateA();
	`, "")
	assert.Nil(t, err)
	resAFirst, err := bridge.GoValue(vAFirst, ctx)
	assert.Nil(t, err)
	firstA := resAFirst.(map[string]interface{})
	assert.True(t, crypto.IsEnvelope(firstA["patient_mobile"].(string)), "态 A: First 查询密文必须原样透传")

	// ==========================================
	// 态 B: YAO_ENGINE_CRYPTO=on (实验组：内核级自动嗅探解密)
	// ==========================================
	os.Setenv("YAO_ENGINE_CRYPTO", "on")
	assert.True(t, crypto.IsEngineCryptoEnabled(), "态 B: 引擎国密开关必须处于开启状态")

	// 态 B - Get 查询
	vB, err := ctx.RunScript(`
		function GetStateB() {
			var q = new Query("query-crypto-test");
			return q.Get({
				"select": ["id", "patient_name", "patient_mobile", "patient_card_no", "age"],
				"from": "query_crypto_test",
				"order": ["id"]
			});
		}
		GetStateB();
	`, "")
	assert.Nil(t, err)
	resB, err := bridge.GoValue(vB, ctx)
	assert.Nil(t, err)

	rowsB, ok := resB.([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 2, len(rowsB))

	// 态 B 断言：第 1 条原为密文的记录，全部透明自动解密为明文！
	rowB1 := rowsB[0].(map[string]interface{})
	assert.Equal(t, "13800138000", rowB1["patient_mobile"], "态 B: 手机号必须透明解密为真实手机号")
	assert.Equal(t, "520101199001011234", rowB1["patient_card_no"], "态 B: 身份证必须透明解密为真实身份证")
	assert.Equal(t, "张三丰", rowB1["patient_name"], "态 B: 姓名必须透明解密为真实姓名")
	assert.False(t, crypto.IsEnvelope(rowB1["patient_mobile"].(string)), "态 B: 出口绝不可带有 v1.SM4. 密文特征")
	assert.Equal(t, float64(45), rowB1["age"], "非涉密数字字段不受任何干扰")

	// 态 B 断言：第 2 条本就是明文的记录，保持 100% 幂等，丝毫无损！
	rowB2 := rowsB[1].(map[string]interface{})
	assert.Equal(t, "13900139000", rowB2["patient_mobile"], "态 B: 原生明文必须保持不变")
	assert.Equal(t, "520101199202022345", rowB2["patient_card_no"], "态 B: 原生明文必须保持不变")
	assert.Equal(t, "李四(明文)", rowB2["patient_name"], "态 B: 原生明文姓名保持不变")

	// 态 B - First 查询
	vBFirst, err := ctx.RunScript(`
		function FirstStateB() {
			var q = new Query("query-crypto-test");
			return q.First({
				"select": ["id", "patient_mobile", "patient_name"],
				"from": "query_crypto_test",
				"where": [{"field": "id", "value": 1}]
			});
		}
		FirstStateB();
	`, "")
	assert.Nil(t, err)
	resBFirst, err := bridge.GoValue(vBFirst, ctx)
	assert.Nil(t, err)
	firstB := resBFirst.(map[string]interface{})
	assert.Equal(t, "13800138000", firstB["patient_mobile"], "态 B: First 查询手机号必须透明解密")
	assert.Equal(t, "张三丰", firstB["patient_name"], "态 B: First 查询姓名必须透明解密")

	// 态 B - Paginate 分页查询
	vBPaginate, err := ctx.RunScript(`
		function PaginateStateB() {
			var q = new Query("query-crypto-test");
			return q.Paginate({
				"select": ["id", "patient_mobile"],
				"from": "query_crypto_test",
				"page": 1,
				"pagesize": 10
			});
		}
		PaginateStateB();
	`, "")
	assert.Nil(t, err)
	resBPaginate, err := bridge.GoValue(vBPaginate, ctx)
	assert.Nil(t, err)
	pageMap := resBPaginate.(map[string]interface{})
	assert.Equal(t, float64(2), pageMap["total"], "分页总数正确")
	items := pageMap["items"].([]interface{})
	assert.Equal(t, 2, len(items))
	item0 := items[0].(map[string]interface{})
	assert.Equal(t, "13800138000", item0["patient_mobile"], "态 B: Paginate 分页 Items 涉密字段自动透明解密")

	// 态 B - SQL 别名查询（信封嗅探模式免疫别名混淆）
	vBAlias, err := ctx.RunScript(`
		function AliasStateB() {
			var q = new Query("query-crypto-test");
			return q.First({
				"select": ["patient_mobile as phone", "patient_card_no as cert_id"],
				"from": "query_crypto_test",
				"where": [{"field": "id", "value": 1}]
			});
		}
		AliasStateB();
	`, "")
	assert.Nil(t, err)
	resBAlias, err := bridge.GoValue(vBAlias, ctx)
	assert.Nil(t, err)
	aliasMap := resBAlias.(map[string]interface{})
	assert.Equal(t, "13800138000", aliasMap["phone"], "态 B: 别名 phone 依然被精准嗅探并解密")
	assert.Equal(t, "520101199001011234", aliasMap["cert_id"], "态 B: 别名 cert_id 依然被精准嗅探并解密")

	// 态 B - query.Run 原生 SQL 查询自动嗅探解密
	vBRun, err := ctx.RunScript(`
		function RunStateB() {
			var q = new Query("query-crypto-test");
			return q.Run({
				"select": ["id", "patient_name", "patient_mobile"],
				"from": "query_crypto_test",
				"limit": 1
			});
		}
		RunStateB();
	`, "")
	assert.Nil(t, err)
	resBRun, err := bridge.GoValue(vBRun, ctx)
	assert.Nil(t, err)
	runRows := resBRun.([]interface{})
	assert.Equal(t, 1, len(runRows))
	runRow0 := runRows[0].(map[string]interface{})
	assert.Equal(t, "张三丰", runRow0["patient_name"], "态 B: query.Run 姓名自动嗅探解密")
	assert.Equal(t, "13800138000", runRow0["patient_mobile"], "态 B: query.Run 手机号自动嗅探解密")
}
