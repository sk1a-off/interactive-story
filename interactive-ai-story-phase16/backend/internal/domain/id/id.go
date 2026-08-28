package id

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

type ID string

var ErrInvalid = errors.New("invalid id")

func New() (ID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	var dst [36]byte
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return ID(dst[:]), nil
}

func Parse(value string) (ID, error) {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return "", ErrInvalid
	}
	compact := value[0:8] + value[9:13] + value[14:18] + value[19:23] + value[24:36]
	if _, err := hex.DecodeString(compact); err != nil {
		return "", ErrInvalid
	}
	return ID(value), nil
}

func MustParse(value string) ID {
	parsed, err := Parse(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func (i ID) String() string { return string(i) }
func (i ID) IsZero() bool   { return i == "" }
