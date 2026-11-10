package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Re-evaluate an immutable raw report without model, HTTP or database calls.
func recheckBrowserReport(input, output string) error {
	inputPath, _ := filepath.Abs(input)
	outputPath, _ := filepath.Abs(output)
	if inputPath == outputPath || output == "" {
		return fmt.Errorf("distinct input and output are required")
	}
	raw, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	var original report
	if err = json.Unmarshal(raw, &original); err != nil {
		return err
	}
	cases := map[string]testCase{}
	for _, tc := range browserRoleSuite() {
		cases[tc.Name] = tc
	}
	for i, c := range original.Cases {
		if !c.OK {
			continue
		}
		tc, ok := cases[c.Name]
		if !ok {
			return fmt.Errorf("unknown frozen case %s", c.Name)
		}
		var obj map[string]any
		if json.Unmarshal([]byte(c.Output), &obj) != nil {
			return fmt.Errorf("invalid saved output")
		}
		original.Cases[i].Validation = tc.Validate(obj, c.Output)
	}
	if len(original.Cases) > 0 {
		total := 0.0
		for _, c := range original.Cases {
			total += caseScore(c.Validation)
		}
		original.AverageScore = total / float64(len(original.Cases))
	}
	hash := sha256.Sum256(raw)
	wrapper := struct {
		SourceSHA256 string `json:"sourceSha256"`
		Validator    string `json:"validator"`
		Report       report `json:"report"`
	}{fmt.Sprintf("%x", hash), "browser-production-gates-v2; offline recheck, no new model calls", original}
	encoded, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(output, append(encoded, '\n'), 0644)
}
