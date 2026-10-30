package model

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncryptorAESDecodeSyntax(t *testing.T) {
	t.Parallel()

	aes := &EncryptorAES{
		Encryptor: &Encryptor{
			Name: "AES",
			Key:  "secret123",
		},
	}

	tests := []struct {
		name     string
		field    string
		expected string
	}{
		{
			name:     "plain column name",
			field:    "password",
			expected: "AES_DECRYPT(UNHEX(`password`), 'secret123')",
		},
		{
			name:     "column name with backticks",
			field:    "`password`",
			expected: "AES_DECRYPT(UNHEX(`password`), 'secret123')",
		},
		{
			name:     "table and column name",
			field:    "users.password",
			expected: "AES_DECRYPT(UNHEX(`users`.`password`), 'secret123')",
		},
		{
			name:     "table and column name with backticks",
			field:    "`users`.`password`",
			expected: "AES_DECRYPT(UNHEX(`users`.`password`), 'secret123')",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := aes.Decode(tt.field)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)

			// 严格验证生成的 SQL 左右括号匹配，杜绝缺少闭合括号致命错误
			leftParens := strings.Count(result, "(")
			rightParens := strings.Count(result, ")")
			assert.Equal(t, leftParens, rightParens, "SQL 表达式中的左右括号必须严格闭合")
			assert.True(t, strings.HasSuffix(result, ")"), "SQL 表达式末尾必须以右括号闭合")
		})
	}
}

func TestEncryptorAESEncodeQuotes(t *testing.T) {
	t.Parallel()

	aes := &EncryptorAES{
		Encryptor: &Encryptor{
			Name: "AES",
			Key:  "sec'ret",
		},
	}

	encoded, err := aes.Encode("val'ue")
	assert.NoError(t, err)
	assert.Equal(t, "HEX(AES_ENCRYPT('val''ue', 'sec''ret'))", encoded)
}

func TestSelectCryptConcurrent(t *testing.T) {
	// 初始化测试加密器
	_, err := WithCrypt([]byte(`{"key": "test_key_aes"}`), "AES")
	assert.NoError(t, err)

	_, err = WithCrypt([]byte(`{"salt": "test_salt_pwd"}`), "PASSWORD")
	assert.NoError(t, err)

	const concurrency = 50
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(concurrency * 2)

	// 并发调用 AES
	for i := 0; i < concurrency; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				enc, err := SelectCrypt("AES")
				if err != nil {
					t.Errorf("worker %d failed to SelectCrypt AES: %v", workerID, err)
					return
				}
				sql, err := enc.Decode(fmt.Sprintf("col_%d", j))
				if err != nil || !strings.HasSuffix(sql, ")") {
					t.Errorf("worker %d invalid decode sql: %s", workerID, sql)
					return
				}
			}
		}(i)
	}

	// 并发调用 PASSWORD
	for i := 0; i < concurrency; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				enc, err := SelectCrypt("PASSWORD")
				if err != nil {
					t.Errorf("worker %d failed to SelectCrypt PASSWORD: %v", workerID, err)
					return
				}
				pwd := fmt.Sprintf("password_%d_%d", workerID, j)
				hash, err := enc.Encode(pwd)
				if err != nil {
					t.Errorf("worker %d failed to encode: %v", workerID, err)
					return
				}
				if !enc.Validate(hash, pwd) {
					t.Errorf("worker %d validate failed", workerID)
					return
				}
			}
		}(i)
	}

	wg.Wait()
}

