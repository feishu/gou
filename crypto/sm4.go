package crypto

import (
	"bytes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// BlockSize SM4 分组长度为 16 字节 (128 位)
const BlockSize = 16

// Sbox SM4 S盒
var sbox = [256]byte{
	0xd6, 0x90, 0xe9, 0xfe, 0xcc, 0xe1, 0x3d, 0xb7, 0x16, 0xb6, 0x14, 0xc2, 0x28, 0xfb, 0x2c, 0x05,
	0x2b, 0x67, 0x9a, 0x76, 0x2a, 0xbe, 0x04, 0xc3, 0xaa, 0x44, 0x13, 0x26, 0x49, 0x86, 0x06, 0x99,
	0x9c, 0x42, 0x50, 0xf4, 0x91, 0xef, 0x98, 0x7a, 0x33, 0x54, 0x0b, 0x43, 0xed, 0xcf, 0xac, 0x62,
	0xe4, 0xb3, 0x1c, 0xa9, 0xc9, 0x08, 0xe8, 0x95, 0x80, 0xdf, 0x94, 0xfa, 0x75, 0x8f, 0x3f, 0xa6,
	0x47, 0x07, 0xa7, 0xfc, 0xf3, 0x73, 0x17, 0xba, 0x83, 0x59, 0x3c, 0x19, 0xe6, 0x85, 0x4f, 0xa8,
	0x68, 0x6b, 0x81, 0xb2, 0x71, 0x64, 0xda, 0x8b, 0xf8, 0xeb, 0x0f, 0x4b, 0x70, 0x56, 0x9d, 0x35,
	0x1e, 0x24, 0x0e, 0x5e, 0x63, 0x58, 0xd1, 0xa2, 0x25, 0x22, 0x7c, 0x3b, 0x01, 0x21, 0x78, 0x87,
	0xd4, 0x00, 0x46, 0x57, 0x9f, 0xd3, 0x27, 0x52, 0x4c, 0x36, 0x02, 0xe7, 0xa0, 0xc4, 0xc8, 0x9e,
	0xea, 0xbf, 0x8a, 0xd2, 0x40, 0xc7, 0x38, 0xb5, 0xa3, 0xf7, 0xf2, 0xce, 0xf9, 0x61, 0x15, 0xa1,
	0xe0, 0xae, 0x5d, 0xa4, 0x9b, 0x34, 0x1a, 0x55, 0xad, 0x93, 0x32, 0x30, 0xf5, 0x8c, 0xb1, 0xe3,
	0x1d, 0xf6, 0xe2, 0x2e, 0x82, 0x66, 0xca, 0x60, 0xc0, 0x29, 0x23, 0xab, 0x0d, 0x53, 0x4e, 0x6f,
	0xd5, 0xdb, 0x37, 0x45, 0xde, 0xfd, 0x8e, 0x2f, 0x03, 0xff, 0x6a, 0x72, 0x6d, 0x6c, 0x5b, 0x51,
	0x8d, 0x1b, 0xaf, 0x92, 0xbb, 0xdd, 0xbc, 0x7f, 0x11, 0xd9, 0x5c, 0x41, 0x1f, 0x10, 0x5a, 0xd8,
	0x0a, 0xc1, 0x31, 0x88, 0xa5, 0xcd, 0x7b, 0xbd, 0x2d, 0x74, 0xd0, 0x12, 0xb8, 0xe5, 0xb4, 0xb0,
	0x89, 0x69, 0x97, 0x4a, 0x0c, 0x96, 0x77, 0x7e, 0x65, 0xb9, 0xf1, 0x09, 0xc5, 0x6e, 0xc6, 0x84,
	0x18, 0xf0, 0x7d, 0xec, 0x3a, 0xdc, 0x4d, 0x20, 0x79, 0xee, 0x5f, 0x3e, 0xd7, 0xcb, 0x39, 0x48,
}

// FK 系统参数
var fk = [4]uint32{0xa3b1bac6, 0x56aa3350, 0x677d9197, 0xb27022dc}

// CK 固定参数表
var ck = [32]uint32{
	0x00070e15, 0x1c232a31, 0x383f464d, 0x545b6269,
	0x70777e85, 0x8c939aa1, 0xa8afb6bd, 0xc4cbd2d9,
	0xe0e7eef5, 0xfc030a11, 0x181f262d, 0x343b4249,
	0x50575e65, 0x6c737a81, 0x888f969d, 0xa4abb2b9,
	0xc0c7ced5, 0xdce3eaf1, 0xf8ff060d, 0x141b2229,
	0x30373e45, 0x4c535a61, 0x686f767d, 0x848b9299,
	0xa0a7aeb5, 0xbcc3cad1, 0xd8dfe6ed, 0xf4fb0209,
	0x10171e25, 0x2c333a41, 0x484f565d, 0x646b7279,
}

// sm4Cipher SM4 密码器结构
type sm4Cipher struct {
	encRoundKeys [32]uint32
	decRoundKeys [32]uint32
}

func rotl(x uint32, n uint) uint32 {
	return (x << n) | (x >> (32 - n))
}

func sm4Lt(b uint32) uint32 {
	t := (uint32(sbox[(b>>24)&0xff]) << 24) |
		(uint32(sbox[(b>>16)&0xff]) << 16) |
		(uint32(sbox[(b>>8)&0xff]) << 8) |
		uint32(sbox[b&0xff])
	return t ^ rotl(t, 2) ^ rotl(t, 10) ^ rotl(t, 18) ^ rotl(t, 24)
}

func sm4Lp(b uint32) uint32 {
	t := (uint32(sbox[(b>>24)&0xff]) << 24) |
		(uint32(sbox[(b>>16)&0xff]) << 16) |
		(uint32(sbox[(b>>8)&0xff]) << 8) |
		uint32(sbox[b&0xff])
	return t ^ rotl(t, 13) ^ rotl(t, 23)
}

// NewSM4Cipher 创建 SM4 密码器，实现 standard cipher.Block 接口
func NewSM4Cipher(key []byte) (cipher.Block, error) {
	if len(key) != BlockSize {
		return nil, fmt.Errorf("crypto: invalid SM4 key size %d, must be 16 bytes", len(key))
	}
	c := &sm4Cipher{}
	var mk [4]uint32
	for i := 0; i < 4; i++ {
		mk[i] = binary.BigEndian.Uint32(key[i*4 : (i+1)*4])
	}
	var k [36]uint32
	k[0] = mk[0] ^ fk[0]
	k[1] = mk[1] ^ fk[1]
	k[2] = mk[2] ^ fk[2]
	k[3] = mk[3] ^ fk[3]

	for i := 0; i < 32; i++ {
		k[i+4] = k[i] ^ sm4Lp(k[i+1]^k[i+2]^k[i+3]^ck[i])
		c.encRoundKeys[i] = k[i+4]
		c.decRoundKeys[31-i] = k[i+4]
	}
	return c, nil
}

func (c *sm4Cipher) BlockSize() int {
	return BlockSize
}

func (c *sm4Cipher) Encrypt(dst, src []byte) {
	if len(src) < BlockSize || len(dst) < BlockSize {
		panic("crypto: buffer size must be at least 16 bytes")
	}
	var x [36]uint32
	for i := 0; i < 4; i++ {
		x[i] = binary.BigEndian.Uint32(src[i*4 : (i+1)*4])
	}
	for i := 0; i < 32; i++ {
		x[i+4] = x[i] ^ sm4Lt(x[i+1]^x[i+2]^x[i+3]^c.encRoundKeys[i])
	}
	binary.BigEndian.PutUint32(dst[0:4], x[35])
	binary.BigEndian.PutUint32(dst[4:8], x[34])
	binary.BigEndian.PutUint32(dst[8:12], x[33])
	binary.BigEndian.PutUint32(dst[12:16], x[32])
}

func (c *sm4Cipher) Decrypt(dst, src []byte) {
	if len(src) < BlockSize || len(dst) < BlockSize {
		panic("crypto: buffer size must be at least 16 bytes")
	}
	var x [36]uint32
	for i := 0; i < 4; i++ {
		x[i] = binary.BigEndian.Uint32(src[i*4 : (i+1)*4])
	}
	for i := 0; i < 32; i++ {
		x[i+4] = x[i] ^ sm4Lt(x[i+1]^x[i+2]^x[i+3]^c.decRoundKeys[i])
	}
	binary.BigEndian.PutUint32(dst[0:4], x[35])
	binary.BigEndian.PutUint32(dst[4:8], x[34])
	binary.BigEndian.PutUint32(dst[8:12], x[33])
	binary.BigEndian.PutUint32(dst[12:16], x[32])
}

// PKCS7Padding 填充
func PKCS7Padding(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...)
}

// PKCS7Unpadding 去填充
func PKCS7Unpadding(data []byte) ([]byte, error) {
	length := len(data)
	if length == 0 {
		return nil, errors.New("pkcs7: empty data")
	}
	if length%BlockSize != 0 {
		return nil, errors.New("pkcs7: invalid padding block size")
	}
	padding := int(data[length-1])
	if padding == 0 || padding > BlockSize || padding > length {
		return nil, errors.New("pkcs7: invalid padding length")
	}
	for i := length - padding; i < length; i++ {
		if data[i] != byte(padding) {
			return nil, errors.New("pkcs7: invalid padding byte")
		}
	}
	return data[:length-padding], nil
}

// SM4CBCEncrypt SM4/CBC/PKCS7 加密并返回 Base64 字符串
func SM4CBCEncrypt(plain []byte, key, iv []byte) (string, error) {
	block, err := NewSM4Cipher(key)
	if err != nil {
		return "", err
	}
	if len(iv) != BlockSize {
		return "", fmt.Errorf("crypto: invalid IV length %d", len(iv))
	}
	padded := PKCS7Padding(plain, BlockSize)
	ciphertext := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// SM4CBCDecrypt SM4/CBC/PKCS7 解密 Base64 密文并返回明文
func SM4CBCDecrypt(cipherBase64 string, key, iv []byte) ([]byte, error) {
	cipherBytes, err := base64.StdEncoding.DecodeString(cipherBase64)
	if err != nil {
		return nil, err
	}
	block, err := NewSM4Cipher(key)
	if err != nil {
		return nil, err
	}
	if len(cipherBytes)%BlockSize != 0 || len(cipherBytes) == 0 {
		return nil, errors.New("crypto: invalid cipher text length")
	}
	if len(iv) != BlockSize {
		return nil, fmt.Errorf("crypto: invalid IV length %d", len(iv))
	}
	plainPadded := make([]byte, len(cipherBytes))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plainPadded, cipherBytes)
	return PKCS7Unpadding(plainPadded)
}

// SM4ECBEncrypt SM4/ECB/PKCS7 加密并返回 Base64 字符串
func SM4ECBEncrypt(plain []byte, key []byte) (string, error) {
	block, err := NewSM4Cipher(key)
	if err != nil {
		return "", err
	}
	padded := PKCS7Padding(plain, BlockSize)
	ciphertext := make([]byte, len(padded))
	for i := 0; i < len(padded); i += BlockSize {
		block.Encrypt(ciphertext[i:i+BlockSize], padded[i:i+BlockSize])
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// SM4ECBDecrypt SM4/ECB/PKCS7 解密 Base64 密文并返回明文
func SM4ECBDecrypt(cipherBase64 string, key []byte) ([]byte, error) {
	cipherBytes, err := base64.StdEncoding.DecodeString(cipherBase64)
	if err != nil {
		return nil, err
	}
	block, err := NewSM4Cipher(key)
	if err != nil {
		return nil, err
	}
	if len(cipherBytes)%BlockSize != 0 || len(cipherBytes) == 0 {
		return nil, errors.New("crypto: invalid cipher text length")
	}
	plainPadded := make([]byte, len(cipherBytes))
	for i := 0; i < len(cipherBytes); i += BlockSize {
		block.Decrypt(plainPadded[i:i+BlockSize], cipherBytes[i:i+BlockSize])
	}
	return PKCS7Unpadding(plainPadded)
}

// =========================================================================
// 国密 SM3 密码杂凑算法 (GB/T 32918)
// =========================================================================

var sm3IV = [8]uint32{
	0x7380166f, 0x4914b2b9, 0x172442d7, 0xda8a0600,
	0xa96f30bc, 0x163138aa, 0xe38dee4d, 0xb0fb0e4e,
}

func sm3P0(x uint32) uint32 {
	return x ^ rotl(x, 9) ^ rotl(x, 17)
}

func sm3P1(x uint32) uint32 {
	return x ^ rotl(x, 15) ^ rotl(x, 23)
}

func sm3FF(x, y, z uint32, j int) uint32 {
	if j < 16 {
		return x ^ y ^ z
	}
	return (x & y) | (x & z) | (y & z)
}

func sm3GG(x, y, z uint32, j int) uint32 {
	if j < 16 {
		return x ^ y ^ z
	}
	return (x & y) | (^x & z)
}

// SM3 计算数据的 256 位 SM3 杂凑值 (32 字节)
func SM3(msg []byte) []byte {
	bitLen := uint64(len(msg)) * 8
	var buf bytes.Buffer
	buf.Write(msg)
	buf.WriteByte(0x80)
	for (buf.Len()+8)%64 != 0 {
		buf.WriteByte(0x00)
	}
	lenBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(lenBytes, bitLen)
	buf.Write(lenBytes)

	padded := buf.Bytes()
	v := sm3IV

	for i := 0; i < len(padded); i += 64 {
		b := padded[i : i+64]
		var w [68]uint32
		var w1 [64]uint32
		for j := 0; j < 16; j++ {
			w[j] = binary.BigEndian.Uint32(b[j*4 : (j+1)*4])
		}
		for j := 16; j < 68; j++ {
			w[j] = sm3P1(w[j-16]^w[j-9]^rotl(w[j-3], 15)) ^ rotl(w[j-13], 7) ^ w[j-6]
		}
		for j := 0; j < 64; j++ {
			w1[j] = w[j] ^ w[j+4]
		}

		a, b0, c, d, e, f, g, h := v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]
		for j := 0; j < 64; j++ {
			var tj uint32 = 0x79cc4519
			if j >= 16 {
				tj = 0x7a879d8a
			}
			ss1 := rotl(rotl(a, 12)+e+rotl(tj, uint(j%32)), 7)
			ss2 := ss1 ^ rotl(a, 12)
			tt1 := sm3FF(a, b0, c, j) + d + ss2 + w1[j]
			tt2 := sm3GG(e, f, g, j) + h + ss1 + w[j]
			d = c
			c = rotl(b0, 9)
			b0 = a
			a = tt1
			h = g
			g = rotl(f, 19)
			f = e
			e = sm3P0(tt2)
		}
		v[0] ^= a
		v[1] ^= b0
		v[2] ^= c
		v[3] ^= d
		v[4] ^= e
		v[5] ^= f
		v[6] ^= g
		v[7] ^= h
	}

	out := make([]byte, 32)
	for i := 0; i < 8; i++ {
		binary.BigEndian.PutUint32(out[i*4:(i+1)*4], v[i])
	}
	return out
}

// SM3Hex 计算 SM3 并以 16 进制字符串返回
func SM3Hex(msg []byte) string {
	digest := SM3(msg)
	return fmt.Sprintf("%x", digest)
}

// HmacSM3 计算 HMAC-SM3
func HmacSM3(key, data []byte) []byte {
	blockSize := 64
	k := make([]byte, blockSize)
	if len(key) > blockSize {
		digest := SM3(key)
		copy(k, digest)
	} else {
		copy(k, key)
	}

	ipad := make([]byte, blockSize)
	opad := make([]byte, blockSize)
	for i := 0; i < blockSize; i++ {
		ipad[i] = k[i] ^ 0x36
		opad[i] = k[i] ^ 0x5c
	}

	inner := append(ipad, data...)
	innerDigest := SM3(inner)

	outer := append(opad, innerDigest...)
	return SM3(outer)
}

// HmacSM3Hex 计算 HMAC-SM3 并返回十六进制字符串
func HmacSM3Hex(key, data []byte) string {
	return fmt.Sprintf("%x", HmacSM3(key, data))
}

// =========================================================================
// 统一自描述密文封套 (Idempotent Envelope Pattern)
// 格式: v1.<alg>.<kid>.<payload>
// =========================================================================

// IsEnvelope 判断字符串是否属于自描述密文封套格式
func IsEnvelope(str string) bool {
	if !strings.HasPrefix(str, "v1.") {
		return false
	}
	parts := strings.Split(str, ".")
	return len(parts) >= 4 && parts[0] == "v1" && len(parts[1]) > 0 && len(parts[2]) > 0 && len(parts[3]) > 0
}

// DecodeEnvelope 解析密文封套: 返回 alg, kid, payload, ok
func DecodeEnvelope(envelope string) (alg, kid, payload string, ok bool) {
	if !IsEnvelope(envelope) {
		return "", "", "", false
	}
	parts := strings.SplitN(envelope, ".", 4)
	if len(parts) < 4 {
		return "", "", "", false
	}
	return parts[1], parts[2], parts[3], true
}

// EncodeEnvelope 组装密文封套
func EncodeEnvelope(alg, kid, payload string) string {
	return fmt.Sprintf("v1.%s.%s.%s", alg, kid, payload)
}

// DecryptEnvelope 自动识别算法与封套并解密，非封套或解密失败返回原值
func DecryptEnvelope(val string, key, iv []byte) (string, error) {
	if !IsEnvelope(val) {
		return val, nil
	}
	alg, _, payload, ok := DecodeEnvelope(val)
	if !ok {
		return val, nil
	}

	switch alg {
	case "SM4/CBC/PKCS7", "SM4/CBC", "SM4":
		plain, err := SM4CBCDecrypt(payload, key, iv)
		if err != nil {
			return val, err
		}
		return string(plain), nil
	case "SM4/ECB/PKCS7", "SM4/ECB":
		plain, err := SM4ECBDecrypt(payload, key)
		if err != nil {
			return val, err
		}
		return string(plain), nil
	case "HMAC-SM3":
		// 不可逆哈希摘要，无解密路径
		return val, nil
	default:
		// 默认回退为 SM4-CBC 解密
		plain, err := SM4CBCDecrypt(payload, key, iv)
		if err == nil {
			return string(plain), nil
		}
		return val, nil
	}
}
