package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Server) encrypt(plain []byte) ([]byte, error) {
	b, e := aes.NewCipher(s.Key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return g.Seal(nonce, nonce, plain, []byte("kaloq-v1-totp")), nil
}
func (s *Server) decrypt(enc []byte) ([]byte, error) {
	b, e := aes.NewCipher(s.Key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	if len(enc) < g.NonceSize() {
		return nil, errors.New("invalid ciphertext")
	}
	return g.Open(nil, enc[:g.NonceSize()], enc[g.NonceSize():], []byte("kaloq-v1-totp"))
}
func totpSecret() string {
	b := make([]byte, 20)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}
func totpCode(secret string, step int64) string {
	key, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if e != nil {
		return ""
	}
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], uint64(step))
	m := hmac.New(sha1.New, key)
	m.Write(data[:])
	h := m.Sum(nil)
	o := h[len(h)-1] & 15
	n := binary.BigEndian.Uint32(h[o:o+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000)
}
func verifyTOTP(secret, code string, last int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	now := time.Now().Unix() / 30
	for _, step := range []int64{now, now - 1, now + 1} {
		if step > last && subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}
