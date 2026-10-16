package netease

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/url"
)

// 网易云 WEAPI 的公开常量：第一层固定密钥与 IV、RSA 公钥（无填充、密钥逆序）。
const (
	weapiPresetKey   = "0CoJUm6Qyw8W8jud"
	weapiIV          = "0102030405060708"
	weapiExponent    = "010001"
	weapiModulus     = "00e0b509f6259df8642dbc35662901477df22677ec152b5ff68ace615bb7b725152b3ab17a876aea8a5aa76d2e417629ec4ee341f56135fccf695280104e0312ecbda92557c93870114af6c9d05c4f7f0c3685b7a46bee255932575cce10b424d813cfe4875d3e82047b97ddef52741d546b8e289dc6935b3ece0462db0a22b8e7"
	weapiKeyAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

func weapiAES(plain []byte, key string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte(weapiIV)).CryptBlocks(out, padded)
	return base64.StdEncoding.EncodeToString(out), nil
}

// weapiRSA 按 WEAPI 规则把逆序密钥当作大整数做无填充 RSA，结果补齐为 256 位十六进制。
func weapiRSA(secret string) string {
	reversed := []byte(secret)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	m := new(big.Int).SetBytes(reversed)
	e, _ := new(big.Int).SetString(weapiExponent, 16)
	n, _ := new(big.Int).SetString(weapiModulus, 16)
	return fmt.Sprintf("%0256x", new(big.Int).Exp(m, e, n))
}

// Form 把明文 JSON 参数加密为 params/encSecKey 表单，供封面和歌词共用。
func Form(payload []byte) (url.Values, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	secret := make([]byte, 16)
	for i, b := range random {
		secret[i] = weapiKeyAlphabet[int(b)%len(weapiKeyAlphabet)]
	}
	first, err := weapiAES(payload, weapiPresetKey)
	if err != nil {
		return nil, err
	}
	params, err := weapiAES([]byte(first), string(secret))
	if err != nil {
		return nil, err
	}
	return url.Values{"params": {params}, "encSecKey": {weapiRSA(string(secret))}}, nil
}
