package openai_compatible

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const maxWriterJSON = 1 << 20

// Incremental, linear scan once the top-level text field is located. Other
// fields, nested text, thought steps and JSON syntax never reach the callback.
type writerTextStream struct {
	raw             []byte
	scan            int
	located, closed bool
	out             strings.Builder
	emitted         int
}

func (p *writerTextStream) feed(fragment string, onText func(string)) error {
	if len(p.raw)+len(fragment) > maxWriterJSON {
		return errors.New("writer stream exceeds size limit")
	}
	p.raw = append(p.raw, fragment...)
	if !p.located {
		offset := 0
		trim := bytes.TrimSpace(p.raw)
		if bytes.HasPrefix(trim, []byte("```")) {
			nl := bytes.IndexByte(p.raw, '\n')
			if nl < 0 {
				return nil
			}
			offset = nl + 1
		}
		decoder := json.NewDecoder(bytes.NewReader(p.raw[offset:]))
		token, err := decoder.Token()
		if err != nil || token != json.Delim('{') {
			return nil
		}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil
			}
			if key == "text" {
				pos := offset + int(decoder.InputOffset())
				for pos < len(p.raw) && strings.ContainsRune(" \r\n\t", rune(p.raw[pos])) {
					pos++
				}
				if pos >= len(p.raw) {
					return nil
				}
				if p.raw[pos] != ':' {
					return nil
				}
				pos++
				for pos < len(p.raw) && strings.ContainsRune(" \r\n\t", rune(p.raw[pos])) {
					pos++
				}
				if pos >= len(p.raw) {
					return nil
				}
				if p.raw[pos] != '"' {
					return errors.New("writer text must be a string")
				}
				p.scan = pos + 1
				p.located = true
				break
			}
			var skip json.RawMessage
			if decoder.Decode(&skip) != nil {
				return nil
			}
		}
	}
	if !p.located || p.closed {
		return nil
	}
	for p.scan < len(p.raw) {
		i := p.scan
		b := p.raw[i]
		if b == '"' {
			p.closed = true
			p.scan++
			break
		}
		if b == '\\' {
			if i+1 >= len(p.raw) {
				break
			}
			escape := p.raw[i+1]
			if escape == 'u' {
				if i+6 > len(p.raw) {
					break
				}
				r, ok := hexRune(p.raw[i+2 : i+6])
				if !ok {
					return errors.New("invalid writer unicode escape")
				}
				consumed := 6
				if r >= 0xd800 && r <= 0xdbff {
					if i+12 > len(p.raw) {
						break
					}
					if p.raw[i+6] != '\\' || p.raw[i+7] != 'u' {
						return errors.New("unpaired writer surrogate")
					}
					low, ok := hexRune(p.raw[i+8 : i+12])
					if !ok || low < 0xdc00 || low > 0xdfff {
						return errors.New("unpaired writer surrogate")
					}
					r = utf16.DecodeRune(r, low)
					consumed = 12
				} else if r >= 0xdc00 && r <= 0xdfff {
					return errors.New("unpaired writer surrogate")
				}
				p.out.WriteRune(r)
				p.scan += consumed
				continue
			}
			decoded, ok := map[byte]byte{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}[escape]
			if !ok {
				return errors.New("invalid writer escape")
			}
			p.out.WriteByte(decoded)
			p.scan += 2
			continue
		}
		if b < 0x20 {
			return errors.New("unescaped writer control character")
		}
		if !utf8.FullRune(p.raw[i:]) {
			break
		}
		r, n := utf8.DecodeRune(p.raw[i:])
		if r == utf8.RuneError && n == 1 {
			return errors.New("invalid writer UTF-8")
		}
		p.out.Write(p.raw[i : i+n])
		p.scan += n
	}
	if p.out.Len() != p.emitted && onText != nil {
		p.emitted = p.out.Len()
		if onText != nil {
			onText(p.out.String())
		}
	}
	return nil
}
func hexRune(value []byte) (rune, bool) {
	var result rune
	for _, c := range value {
		result *= 16
		switch {
		case c >= '0' && c <= '9':
			result += rune(c - '0')
		case c >= 'a' && c <= 'f':
			result += rune(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			result += rune(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return result, true
}
func (p *writerTextStream) finish(onText func(string)) ([]byte, error) {
	if p.located && !p.closed {
		return nil, errors.New("writer text string incomplete")
	}
	raw, ok := normalizeJSONObject(string(p.raw))
	if !ok {
		return nil, errors.New("writer stream did not complete a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	_, _ = decoder.Token()
	seen := map[string]bool{}
	var text string
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name := key.(string)
		if seen[name] {
			return nil, errors.New("duplicate writer output key")
		}
		seen[name] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, errors.New("invalid writer output")
		}
		if name == "text" {
			if json.Unmarshal(value, &text) != nil {
				return nil, errors.New("invalid writer text")
			}
		}
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("empty writer text")
	}
	if (text != p.out.String() || p.out.Len() != p.emitted) && onText != nil {
		onText(text)
	}
	return raw, nil
}
