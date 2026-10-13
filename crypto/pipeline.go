package crypto

import (
	"reflect"

	"github.com/yaoapp/gou/query/share"
	"github.com/yaoapp/kun/maps"
)

// EncryptMap 对 Map 按照表策略执行加密转换 (委托给当前 Provider 执行)
func EncryptMap(tableOrModel string, row map[string]interface{}) map[string]interface{} {
	if !IsEngineCryptoEnabled() || IsWhiteListed(tableOrModel) || row == nil {
		return row
	}

	provider := GetActiveProvider()
	if !provider.IsLocal() {
		return row
	}

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
			if encrypted, err := provider.Encrypt(strVal, rule); err == nil {
				row[col] = encrypted
			}
		} else if rule.Op == "hash" {
			if hashed, err := provider.Hash(strVal, rule); err == nil {
				row[col] = hashed
			}
		}
	}
	return row
}

// DecryptMap 对 Map 包含的密文封套执行解密转换 (委托给当前 Provider 执行)
func DecryptMap(tableOrModel string, row map[string]interface{}) map[string]interface{} {
	if !IsEngineCryptoEnabled() || row == nil {
		return row
	}

	provider := GetActiveProvider()
	if !provider.IsLocal() {
		return row
	}

	for col, val := range row {
		strVal, ok := val.(string)
		if !ok || !IsEnvelope(strVal) {
			continue
		}
		if plain, err := provider.Decrypt(strVal); err == nil {
			row[col] = plain
		}
	}
	return row
}

// SniffAndDecrypt 递归嗅探并解密任意数据结构中的国密封套密文 (委托给当前 Provider 执行)
func SniffAndDecrypt(val interface{}) interface{} {
	if !IsEngineCryptoEnabled() || val == nil {
		return val
	}

	provider := GetActiveProvider()
	if !provider.IsLocal() {
		return val
	}

	switch v := val.(type) {
	case string:
		if IsEnvelope(v) {
			if plain, err := provider.Decrypt(v); err == nil {
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
