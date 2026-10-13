package model

import (
	"github.com/yaoapp/gou/crypto"
)

// getModelTableName 获取模型对应的真实物理表名，若未找到则回退为 modelID
func getModelTableName(modelID string) string {
	rwlock.RLock()
	mod, ok := Models[modelID]
	rwlock.RUnlock()
	if ok && mod != nil && mod.MetaData.Table.Name != "" {
		return mod.MetaData.Table.Name
	}
	return modelID
}

// beforePersistEncrypt 在模型写操作（save, create, update, updatewhere）前透明执行涉密列国密加密
func beforePersistEncrypt(modelID string, row map[string]interface{}) map[string]interface{} {
	if !crypto.IsEngineCryptoEnabled() || crypto.IsWhiteListed(modelID) || row == nil {
		return row
	}
	tableName := getModelTableName(modelID)
	return crypto.EncryptMap(tableName, row)
}

// beforeInsertEncrypt 在模型二维批量写入前透明执行涉密列国密加密
func beforeInsertEncrypt(modelID string, columns []string, rows [][]interface{}) ([]string, [][]interface{}) {
	if !crypto.IsEngineCryptoEnabled() || crypto.IsWhiteListed(modelID) || len(rows) == 0 {
		return columns, rows
	}

	tableName := getModelTableName(modelID)
	for rIdx := range rows {
		if len(rows[rIdx]) == len(columns) {
			rowMap := make(map[string]interface{}, len(columns))
			for cIdx, col := range columns {
				rowMap[col] = rows[rIdx][cIdx]
			}
			encMap := crypto.EncryptMap(tableName, rowMap)
			for cIdx, col := range columns {
				rows[rIdx][cIdx] = encMap[col]
			}
		}
	}
	return columns, rows
}

// afterQueryDecrypt 在模型读操作（find, get, paginate）后透明执行信封嗅探与国密解密
func afterQueryDecrypt(modelID string, data interface{}) interface{} {
	if !crypto.IsEngineCryptoEnabled() || crypto.IsWhiteListed(modelID) || data == nil {
		return data
	}
	return crypto.SniffAndDecrypt(data)
}
