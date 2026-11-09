package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
)

type questPromptOverride struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

func overrideQuestPrompt(revision *promptset.Revision, path string) (*questPromptOverride, error) {
	return overrideRolePrompt(revision, path, "quest_evaluator")
}

func overrideRolePrompt(revision *promptset.Revision, path, roleName string) (*questPromptOverride, error) {
	if path == "" {
		return nil, nil
	}
	paths := strings.Split(path, ",")
	absolutePaths := make([]string, 0, len(paths))
	parts := make([][]byte, 0, len(paths))
	for _, item := range paths {
		absolute, err := filepath.Abs(strings.TrimSpace(item))
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(absolute)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(raw)) == "" {
			return nil, fmt.Errorf("quest prompt file is empty")
		}
		absolutePaths = append(absolutePaths, absolute)
		parts = append(parts, raw)
	}
	raw := []byte(strings.Join(byteStrings(parts), "\n"))
	// Clone rather than mutate a map that another revision holder may share.
	values := make(map[string]string, len(revision.Prompts))
	for role, value := range revision.Prompts {
		values[role] = value
	}
	values[roleName] = string(raw)
	revision.Prompts = values
	return &questPromptOverride{File: strings.Join(absolutePaths, ","), SHA256: fmt.Sprintf("%x", sha256.Sum256(raw))}, nil
}

func byteStrings(values [][]byte) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}
