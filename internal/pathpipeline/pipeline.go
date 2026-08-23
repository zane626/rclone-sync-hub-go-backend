// Package pathpipeline compiles and executes upload-path transformation steps.
// A pipeline starts with the source file name and returns a safe relative
// directory that the scanner inserts below the configured remote route.
package pathpipeline

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"rclone-sync-hub/internal/model"
)

const (
	maxSteps         = 16
	maxPatternLength = 4096
	maxOutputLength  = 512
)

type compiledStep interface {
	Apply(input string) (output string, matched bool)
}

type stepCompiler func(model.UploadPathPipelineStep) (compiledStep, error)

// The registry keeps step dispatch independent from the scanner. Adding a new
// transform only requires a model type, compiler registration, and its tests.
var stepCompilers = map[string]stepCompiler{
	model.UploadPathPipelineStepRegexExtract: compileRegexExtract,
}

// Pipeline is an immutable, compiled upload-path pipeline and is safe to reuse
// for every file in one scan.
type Pipeline struct {
	steps []compiledStep
}

// Normalize validates API/storage configuration and returns its canonical form.
func Normalize(steps []model.UploadPathPipelineStep) ([]model.UploadPathPipelineStep, error) {
	if len(steps) > maxSteps {
		return nil, fmt.Errorf("path pipeline may contain at most %d steps", maxSteps)
	}
	if len(steps) == 0 {
		return []model.UploadPathPipelineStep{}, nil
	}

	normalized := make([]model.UploadPathPipelineStep, 0, len(steps))
	for index, step := range steps {
		step.Type = strings.TrimSpace(step.Type)
		step.Pattern = strings.TrimSpace(step.Pattern)
		compiler, supported := stepCompilers[step.Type]
		if !supported {
			return nil, fmt.Errorf("path pipeline step %d has unsupported type %q", index+1, step.Type)
		}
		if _, err := compiler(step); err != nil {
			return nil, fmt.Errorf("path pipeline step %d: %w", index+1, err)
		}
		normalized = append(normalized, step)
	}
	return normalized, nil
}

// Compile validates and compiles steps once so regexes are not recompiled for
// every file visited by the scanner.
func Compile(steps []model.UploadPathPipelineStep) (Pipeline, error) {
	normalized, err := Normalize(steps)
	if err != nil {
		return Pipeline{}, err
	}
	compiled := make([]compiledStep, 0, len(normalized))
	for _, step := range normalized {
		compiler := stepCompilers[step.Type]
		item, compileErr := compiler(step)
		if compileErr != nil {
			return Pipeline{}, compileErr
		}
		compiled = append(compiled, item)
	}
	return Pipeline{steps: compiled}, nil
}

// Resolve runs the pipeline with fileName as its initial input. A regex miss is
// not an error: matched=false tells the scanner to preserve its normal target.
func (p Pipeline) Resolve(fileName string) (directory string, matched bool, err error) {
	if len(p.steps) == 0 {
		return "", false, nil
	}
	value := fileName
	for _, step := range p.steps {
		var stepMatched bool
		value, stepMatched = step.Apply(value)
		if !stepMatched {
			return "", false, nil
		}
	}
	directory, err = normalizeOutput(value)
	if err != nil {
		return "", false, err
	}
	return directory, true, nil
}

type regexExtractStep struct {
	pattern *regexp.Regexp
	group   int
}

func compileRegexExtract(step model.UploadPathPipelineStep) (compiledStep, error) {
	if step.Pattern == "" {
		return nil, errors.New("regex pattern is required")
	}
	if len(step.Pattern) > maxPatternLength {
		return nil, fmt.Errorf("regex pattern must not exceed %d bytes", maxPatternLength)
	}
	pattern, err := regexp.Compile(step.Pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}
	if step.Group < 1 || step.Group > pattern.NumSubexp() {
		return nil, fmt.Errorf("regex group must be between 1 and %d", pattern.NumSubexp())
	}
	return regexExtractStep{pattern: pattern, group: step.Group}, nil
}

func (s regexExtractStep) Apply(input string) (string, bool) {
	matches := s.pattern.FindStringSubmatch(input)
	if len(matches) <= s.group {
		return "", false
	}
	return matches[s.group], true
}

func normalizeOutput(raw string) (string, error) {
	value := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if value == "" {
		return "", errors.New("path pipeline produced an empty directory")
	}
	if strings.ContainsRune(value, '\x00') || strings.HasPrefix(value, "/") {
		return "", errors.New("path pipeline produced an unsafe directory")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("path pipeline directory must stay below the remote route")
	}
	if len(cleaned) > maxOutputLength {
		return "", fmt.Errorf("path pipeline directory must not exceed %d bytes", maxOutputLength)
	}
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == "" || segment == "." || segment == ".." || len(segment) > 255 {
			return "", errors.New("path pipeline produced an invalid directory segment")
		}
	}
	return cleaned, nil
}
