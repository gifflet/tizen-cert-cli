package profile

import (
	"bytes"
	"crypto/des"
	"encoding/base64"
	"fmt"
)

// cipherKey is the static key used by Tizen Studio's CipherUtil for the
// inline password form in profiles.xml (3DES consumes the first 24 bytes).
// Extracted from org.tizen.common's CipherUtil in the 2025 builds.
const cipherKey = "KYANINYLhijklmnopqrstuvwx"

// EncryptPassword produces the inline encrypted password accepted by the
// Tizen signing tools in profiles.xml: 3DES/ECB/PKCS5 + Base64.
func EncryptPassword(plain string) (string, error) {
	block, err := des.NewTripleDESCipher([]byte(cipherKey)[:24])
	if err != nil {
		return "", err
	}
	data := pkcs5Pad([]byte(plain), block.BlockSize())
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += block.BlockSize() {
		block.Encrypt(out[i:], data[i:i+block.BlockSize()])
	}
	return base64.StdEncoding.EncodeToString(out), nil
}

// DecryptPassword reverses EncryptPassword; used in tests and to inspect
// existing profiles.
func DecryptPassword(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := des.NewTripleDESCipher([]byte(cipherKey)[:24])
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw)%block.BlockSize() != 0 {
		return "", fmt.Errorf("ciphertext is not a multiple of the 3DES block size")
	}
	out := make([]byte, len(raw))
	for i := 0; i < len(raw); i += block.BlockSize() {
		block.Decrypt(out[i:], raw[i:i+block.BlockSize()])
	}
	return string(pkcs5Unpad(out)), nil
}

func pkcs5Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(padding)}, padding)...)
}

func pkcs5Unpad(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > len(data) {
		return data
	}
	return data[:len(data)-padding]
}
