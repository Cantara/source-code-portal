package people

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

// sealInfo names what the key derived from enterprise.token_key_file is for: no other use of that file derives the same key.
const sealInfo = "scp enterprise: people's GitHub tokens, v1"

// sealer seals the people's tokens with AES-256-GCM under a key derived (HKDF-SHA256) from the configured key. The person, source and
// owner are the additional data, so a sealed token moved to another person's row does not open.
type sealer struct{ aead cipher.AEAD }

func newSealer(key []byte) (*sealer, error) {
	if len(key) < 32 {
		return nil, errors.New("people: the token key is shorter than 32 bytes")
	}
	k, err := hkdf.Key(sha256.New, key, nil, sealInfo, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &sealer{aead: aead}, nil
}

func additional(person, source, owner string) []byte {
	return []byte(person + "\x00" + source + "\x00" + owner)
}

func (s *sealer) seal(person, source, owner string, token []byte) []byte {
	nonce := make([]byte, s.aead.NonceSize())
	_, _ = rand.Read(nonce) // crypto/rand.Read never fails
	return s.aead.Seal(nonce, nonce, token, additional(person, source, owner))
}

func (s *sealer) open(person, source, owner string, sealed []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("people: a sealed token too short")
	}
	return s.aead.Open(nil, sealed[:n], sealed[n:], additional(person, source, owner))
}
