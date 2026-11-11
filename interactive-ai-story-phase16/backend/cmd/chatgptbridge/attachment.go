package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const attachmentFormat = "attachment-txt-v1"

type promptAttachment struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
	Bytes   int    `json:"bytes"`
}

func textAttachment(in completionRequest, id string) (string, *promptAttachment) {
	var out strings.Builder
	fmt.Fprintf(&out, "STORY REQUEST ID: %s\n", id)
	for _, m := range in.Messages {
		if m.Role == "system" {
			out.WriteString("\nSYSTEM INSTRUCTIONS\n")
		} else {
			out.WriteString("\nUSER INPUT\n")
		}
		// Indent JSON without parsing numbers or changing any original value.
		var formatted bytes.Buffer
		if m.Role == "user" && json.Indent(&formatted, []byte(m.Content), "", "  ") == nil {
			out.Write(formatted.Bytes())
		} else {
			out.WriteString(m.Content)
		}
		out.WriteByte('\n')
	}
	if len(in.ResponseFormat) > 0 {
		out.WriteString("\nRESPONSE FORMAT\n")
		out.Write(in.ResponseFormat)
		out.WriteByte('\n')
	}
	content := out.String()
	sum := sha256.Sum256([]byte(content))
	file := &promptAttachment{Name: "story-" + id + ".txt", Content: content, SHA256: hex.EncodeToString(sum[:]), Bytes: len(content)}
	prompt := fmt.Sprintf("Story request %s. Read the attached UTF-8 file %s in full. Execute its SYSTEM INSTRUCTIONS using its USER INPUT and RESPONSE FORMAT. Return exactly one JSON object, without Markdown or explanations. Do not browse the web. If the file is unavailable, do not invent a result.", id, file.Name)
	return prompt, file
}
func supportsAttachment(version string) bool { return version == "0.4.0" }
func supportsProject(version string) bool    { return version == "0.3.0" || supportsAttachment(version) }
