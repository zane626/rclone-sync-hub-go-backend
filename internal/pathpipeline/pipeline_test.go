package pathpipeline

import (
	"testing"

	"rclone-sync-hub/internal/model"
)

func TestRegexExtractResolvesExampleMonth(t *testing.T) {
	pipeline, err := Compile([]model.UploadPathPipelineStep{{
		Type:    model.UploadPathPipelineStepRegexExtract,
		Pattern: `\[(\d{4}-\d{2})-\d{2}_`,
		Group:   1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	directory, matched, err := pipeline.Resolve(`[Zz1tai]-直播回放-[2025-11-19_21_01_27].mp4`)
	if err != nil || !matched || directory != "2025-11" {
		t.Fatalf("directory=%q matched=%v err=%v, want 2025-11/true/nil", directory, matched, err)
	}
}

func TestRegexExtractMissFallsBack(t *testing.T) {
	pipeline, err := Compile([]model.UploadPathPipelineStep{{
		Type: model.UploadPathPipelineStepRegexExtract, Pattern: `\[(\d{4}-\d{2})-`, Group: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	directory, matched, err := pipeline.Resolve("plain-name.mp4")
	if err != nil || matched || directory != "" {
		t.Fatalf("directory=%q matched=%v err=%v, want empty/false/nil", directory, matched, err)
	}
}

func TestNormalizeRejectsInvalidStepConfiguration(t *testing.T) {
	tests := []struct {
		name string
		step model.UploadPathPipelineStep
	}{
		{name: "unknown type", step: model.UploadPathPipelineStep{Type: "script"}},
		{name: "invalid regex", step: model.UploadPathPipelineStep{Type: model.UploadPathPipelineStepRegexExtract, Pattern: `(`, Group: 1}},
		{name: "missing capture", step: model.UploadPathPipelineStep{Type: model.UploadPathPipelineStepRegexExtract, Pattern: `\d+`, Group: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Normalize([]model.UploadPathPipelineStep{test.step}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestResolveRejectsTraversalOutput(t *testing.T) {
	pipeline, err := Compile([]model.UploadPathPipelineStep{{
		Type: model.UploadPathPipelineStepRegexExtract, Pattern: `^(.*)$`, Group: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pipeline.Resolve("../outside"); err == nil {
		t.Fatal("expected unsafe output error")
	}
}
