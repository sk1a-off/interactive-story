package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriterStreamingSmokeFakeKeepsBothAvailabilityModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "writer.json")
	if err := runWriterStreamSmoke(turnBenchmarkOptions{Provider: "fake", Repeats: 1}, path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var r struct {
		Status string                 `json:"status"`
		Runs   []writerStreamSmokeRow `json:"runs"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Status != "smoke_only" || len(r.Runs) != 2 {
		t.Fatal("invalid fake stream smoke")
	}
	if r.Runs[0].Snapshots != 0 || r.Runs[1].Snapshots == 0 {
		t.Fatal("stream mode not observed")
	}
	for _, row := range r.Runs {
		if !row.Passed || row.Choices != 4 {
			t.Fatal("pipeline compatibility failed")
		}
		for _, call := range row.Calls {
			if call.HTTPPosts != 0 {
				t.Fatal("fake smoke reached provider")
			}
		}
	}
}
