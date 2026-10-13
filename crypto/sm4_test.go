package crypto

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestSM4StandardVectors 国家标准 GB/T 32907-2016 官方标准测试用例
func TestSM4StandardVectors(t *testing.T) {
	// 官方测试向量
	keyHex := "0123456789abcdeffedcba9876543210"
	plainHex := "0123456789abcdeffedcba9876543210"
	expectedCipherHex := "681edf34d206965e86b3e94f536e4246"

	key, _ := hex.DecodeString(keyHex)
	plain, _ := hex.DecodeString(plainHex)
	expectedCipher, _ := hex.DecodeString(expectedCipherHex)

	cipher, err := NewSM4Cipher(key)
	if err != nil {
		t.Fatalf("NewSM4Cipher error: %v", err)
	}

	dst := make([]byte, BlockSize)
	cipher.Encrypt(dst, plain)

	if !bytes.Equal(dst, expectedCipher) {
		t.Fatalf("SM4 加密输出与国家标准不一致! 期望 %s, 得到 %s", expectedCipherHex, hex.EncodeToString(dst))
	}

	// 解密测试
	decDst := make([]byte, BlockSize)
	cipher.Decrypt(decDst, dst)
	if !bytes.Equal(decDst, plain) {
		t.Fatalf("SM4 解密输出与明文不一致! 期望 %s, 得到 %s", plainHex, hex.EncodeToString(decDst))
	}
}

// TestSM4CBCRoundtrip 测试 SM4/CBC/PKCS7 完整加解密
func TestSM4CBCRoundtrip(t *testing.T) {
	key := []byte("SydLocalSm4Key01")
	iv := []byte("SydLocalSm4Iv016")
	plainText := "用户手机号: 13912345678, 身份证: 520101199001011234"

	cipherB64, err := SM4CBCEncrypt([]byte(plainText), key, iv)
	if err != nil {
		t.Fatalf("SM4CBCEncrypt failed: %v", err)
	}

	decrypted, err := SM4CBCDecrypt(cipherB64, key, iv)
	if err != nil {
		t.Fatalf("SM4CBCDecrypt failed: %v", err)
	}

	if string(decrypted) != plainText {
		t.Fatalf("CBC 解密结果不匹配! 期望 %s, 实际 %s", plainText, string(decrypted))
	}
}

// TestSM4ECBRoundtrip 测试 SM4/ECB/PKCS7 完整加解密
func TestSM4ECBRoundtrip(t *testing.T) {
	key := []byte("SydLocalSm4Key01")
	plainText := "硬件密码机用户手机: 13888889999"

	cipherB64, err := SM4ECBEncrypt([]byte(plainText), key)
	if err != nil {
		t.Fatalf("SM4ECBEncrypt failed: %v", err)
	}

	decrypted, err := SM4ECBDecrypt(cipherB64, key)
	if err != nil {
		t.Fatalf("SM4ECBDecrypt failed: %v", err)
	}

	if string(decrypted) != plainText {
		t.Fatalf("ECB 解密结果不匹配! 期望 %s, 实际 %s", plainText, string(decrypted))
	}
}

// TestSM3StandardVector 测试 SM3 官方标准测试向量
func TestSM3StandardVector(t *testing.T) {
	// "abc" -> 66c7f0f4 62eeedd9 d1f2d46b dc10e4e2 4167c487 5cf2f7a2 297da02b 8f4ba8e0
	expected := "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0"
	got := SM3Hex([]byte("abc"))
	if got != expected {
		t.Fatalf("SM3 杂凑结果与标准不一致! 期望 %s, 实际 %s", expected, got)
	}
}

// TestEnvelopeOperations 测试密文封套编解码与自动解密
func TestEnvelopeOperations(t *testing.T) {
	key := []byte("SydLocalSm4Key01")
	iv := []byte("SydLocalSm4Iv016")

	// CBC 封套测试
	plain := "13800138000"
	b64, err := SM4CBCEncrypt([]byte(plain), key, iv)
	if err != nil {
		t.Fatalf("SM4CBCEncrypt failed: %v", err)
	}

	envelope := EncodeEnvelope("SM4/CBC/PKCS7", "local-sm4-1", b64)
	if !IsEnvelope(envelope) {
		t.Fatalf("IsEnvelope failed for %s", envelope)
	}

	decrypted, err := DecryptEnvelope(envelope, key, iv)
	if err != nil {
		t.Fatalf("DecryptEnvelope failed: %v", err)
	}
	if decrypted != plain {
		t.Fatalf("封套解密不匹配: 期望 %s, 得到 %s", plain, decrypted)
	}

	// 幂等性测试: 非封套原样返回
	nonEnv := "明文字符串_not_an_envelope"
	idem, err := DecryptEnvelope(nonEnv, key, iv)
	if err != nil || idem != nonEnv {
		t.Fatalf("非封套数据未原样返回: %s", idem)
	}
}
