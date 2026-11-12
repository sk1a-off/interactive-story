package projection

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// CanonicalHash hashes the information content of a state independently of
// the Go struct layout: object keys are sorted and zero-valued members are
// removed recursively. Adding a new field to a state type therefore does not
// change the hash of previously persisted states, and PostgreSQL jsonb
// normalization cannot affect it either.
func CanonicalHash(state TimelineState) (string, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return CanonicalHashJSON(raw)
}

// CanonicalHashJSON applies the same canonical form to already-serialized JSON.
func CanonicalHashJSON(raw []byte) (string, error) {
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// CanonicalizeJSON returns a deterministic encoding of raw with sorted object
// keys and zero-valued object members elided (null, "", 0, false, empty
// arrays and empty objects). Array elements are never elided because their
// positions carry meaning.
func CanonicalizeJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("canonicalize json: %w", err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("canonicalize json: trailing data")
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func isZeroJSON(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case bool:
		return !v
	case json.Number:
		return numberIsZero(v)
	case []any:
		return len(v) == 0
	case map[string]any:
		for _, member := range v {
			if !isZeroJSON(member) {
				return false
			}
		}
		return true
	}
	return false
}

func numberIsZero(n json.Number) bool {
	s := n.String()
	if s == "" {
		return true
	}
	// Any representation of zero: 0, -0, 0.0, 0e5, 0E-2.
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '0', '-', '+', '.':
			continue
		case 'e', 'E':
			return true
		default:
			return false
		}
	}
	return true
}

// canonicalNumber normalizes numeric spelling so that Go's encoder output and
// PostgreSQL's jsonb rendering (for example 1e-07 versus 0.0000001) agree.
func canonicalNumber(n json.Number) string {
	if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
		return strconv.FormatInt(i, 10)
	}
	if f, err := strconv.ParseFloat(n.String(), 64); err == nil {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return n.String()
}

func writeCanonical(buf *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key, member := range v {
			if isZeroJSON(member) {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			encodedKey, err := json.Marshal(key)
			if err != nil {
				return err
			}
			buf.Write(encodedKey)
			buf.WriteByte(':')
			if err := writeCanonical(buf, v[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		for i, member := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, member); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case json.Number:
		buf.WriteString(canonicalNumber(v))
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(encoded)
	}
	return nil
}
