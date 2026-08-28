package imagegeneration

import (
	"encoding/base64"
	"errors"
	"strings"
)

const MaxDecodedImageBytes = 15 << 20

var ErrInvalidImagePayload = errors.New("invalid worker image payload")

type DecodedImage struct {
	ContentType, Extension string
	Data                   []byte
}

func DecodeDataURL(v string) (DecodedImage, error) {
	comma := strings.IndexByte(v, ',')
	if comma < 0 {
		return DecodedImage{}, ErrInvalidImagePayload
	}
	meta, payload := v[:comma], v[comma+1:]
	if !strings.HasSuffix(meta, ";base64") {
		return DecodedImage{}, ErrInvalidImagePayload
	}
	mime := strings.TrimPrefix(strings.TrimSuffix(meta, ";base64"), "data:")
	ext := ""
	switch mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	default:
		return DecodedImage{}, ErrInvalidImagePayload
	}
	if len(payload) > ((MaxDecodedImageBytes+2)/3*4)+16 {
		return DecodedImage{}, ErrInvalidImagePayload
	}
	raw, e := base64.StdEncoding.DecodeString(payload)
	if e != nil || len(raw) == 0 || len(raw) > MaxDecodedImageBytes {
		return DecodedImage{}, ErrInvalidImagePayload
	}
	valid := false
	switch mime {
	case "image/jpeg":
		valid = len(raw) >= 3 && raw[0] == 0xff && raw[1] == 0xd8 && raw[2] == 0xff
	case "image/png":
		valid = len(raw) >= 8 && string(raw[:8]) == "\x89PNG\r\n\x1a\n"
	case "image/webp":
		valid = len(raw) >= 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP"
	}
	if !valid {
		return DecodedImage{}, ErrInvalidImagePayload
	}
	return DecodedImage{ContentType: mime, Extension: ext, Data: raw}, nil
}
