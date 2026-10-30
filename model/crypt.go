package model

import (
	"fmt"
	"strings"
	"sync"

	jsoniter "github.com/json-iterator/go"
	"golang.org/x/crypto/bcrypt"
)

var (
	encryptorsMu sync.RWMutex
	// Encryptors 已加载加密器
	Encryptors = map[string]*Encryptor{}
)

// IEncryptors 加密码器接口映射（保留接口映射，但不在并发时直接修改内部指针）
var IEncryptors = map[string]IEncryptor{
	"AES":      &EncryptorAES{},
	"PASSWORD": &EncryptorPassword{},
}

// WithCrypt 载入数据加密器
func WithCrypt(data []byte, name string) (*Encryptor, error) {
	encryptor := Encryptor{}
	err := jsoniter.Unmarshal(data, &encryptor)
	if err != nil {
		return nil, err
	}
	encryptor.Name = name

	encryptorsMu.Lock()
	defer encryptorsMu.Unlock()
	Encryptors[name] = &encryptor
	return Encryptors[name], nil
}

// SelectCrypt 选择加密器（线程安全实例工厂：每次返回独立实例，杜绝并发竞争与指针篡改）
func SelectCrypt(name string) (IEncryptor, error) {
	encryptorsMu.RLock()
	encryptor, has := Encryptors[name]
	encryptorsMu.RUnlock()
	if !has {
		return nil, fmt.Errorf("加密器:%s; 尚未加载", name)
	}

	switch strings.ToUpper(name) {
	case "AES":
		return &EncryptorAES{Encryptor: encryptor}, nil
	case "PASSWORD":
		return &EncryptorPassword{Encryptor: encryptor}, nil
	}

	encryptorsMu.RLock()
	iencryptor, has := IEncryptors[name]
	encryptorsMu.RUnlock()
	if !has {
		return nil, fmt.Errorf("加密器:%s; 尚不支持", name)
	}

	switch iencryptor.(type) {
	case *EncryptorAES:
		return &EncryptorAES{Encryptor: encryptor}, nil
	case *EncryptorPassword:
		return &EncryptorPassword{Encryptor: encryptor}, nil
	default:
		// 自定义扩展加密器：通过实例克隆保证并发隔离
		clone := &EncryptorAES{Encryptor: encryptor}
		return clone, nil
	}
}

// EncryptorAES AES
type EncryptorAES struct{ *Encryptor }

// EncryptorAES256 AES 256
type EncryptorAES256 struct{ Encryptor }

// EncryptorAES128 AES 128
type EncryptorAES128 struct{ Encryptor }

// EncryptorPassword 密码加密
type EncryptorPassword struct{ *Encryptor }

// Set AES Encode
func (aes *EncryptorAES) Set(crypt *Encryptor) {
	aes.Encryptor = crypt
}

// Encode AES Encode
func (aes EncryptorAES) Encode(value string) (string, error) {
	safeValue := strings.ReplaceAll(value, "'", "''")
	safeKey := strings.ReplaceAll(aes.Key, "'", "''")
	return fmt.Sprintf("HEX(AES_ENCRYPT('%s', '%s'))", safeValue, safeKey), nil
}

// Decode AES Decode
func (aes EncryptorAES) Decode(field string) (string, error) {
	safeKey := strings.ReplaceAll(aes.Key, "'", "''")
	if strings.Contains(field, ".") {
		namer := strings.Split(field, ".")
		table := strings.Trim(namer[0], "`")
		col := strings.Trim(namer[1], "`")
		return fmt.Sprintf("AES_DECRYPT(UNHEX(`%s`.`%s`), '%s')", table, col, safeKey), nil
	}
	cleanField := strings.Trim(field, "`")
	return fmt.Sprintf("AES_DECRYPT(UNHEX(`%s`), '%s')", cleanField, safeKey), nil
}

// Validate AES Decode
func (aes EncryptorAES) Validate(hash string, field string) bool {
	plain, err := aes.Decode(hash)
	if err != nil {
		return false
	}
	return plain == field
}

// Set AES Encode
func (pwd *EncryptorPassword) Set(crypt *Encryptor) {
	pwd.Encryptor = crypt
}

// Encode PASSWORD Encode
func (pwd EncryptorPassword) Encode(value string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.MinCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// Validate PASSWORD Decode
func (pwd EncryptorPassword) Validate(hash string, value string) bool {
	byteHash := []byte(hash)
	err := bcrypt.CompareHashAndPassword(byteHash, []byte(value))
	if err != nil {
		return false
	}
	return true
}

// Decode PASSWORD Decode
func (pwd EncryptorPassword) Decode(value string) (string, error) {
	return value, nil
}
