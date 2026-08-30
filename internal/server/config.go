package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const DefaultTargetVersion = "9.1.0000"
const MaximumTargetVersion = "9.2.1015"

var ErrInvalidTargetVersion = errors.New("invalid Vim target version")

type TargetVersion struct {
	Major  int
	Minor  int
	Patch  int
	Latest bool
}

func (v TargetVersion) String() string {
	if v.Latest {
		return "latest"
	}
	return fmt.Sprintf("%d.%d.%04d", v.Major, v.Minor, v.Patch)
}

func ParseTargetVersion(value string) (TargetVersion, error) {
	if value == "latest" {
		return TargetVersion{Latest: true}, nil
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 && len(parts) != 3 {
		return TargetVersion{}, fmt.Errorf("%w: expected major.minor[.patch] or latest", ErrInvalidTargetVersion)
	}
	numbers := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return TargetVersion{}, fmt.Errorf("%w: empty version component", ErrInvalidTargetVersion)
		}
		for _, b := range []byte(part) {
			if b < '0' || b > '9' {
				return TargetVersion{}, fmt.Errorf("%w: non-decimal version component", ErrInvalidTargetVersion)
			}
		}
		number, err := strconv.Atoi(part)
		if err != nil {
			return TargetVersion{}, fmt.Errorf("%w: %v", ErrInvalidTargetVersion, err)
		}
		numbers[i] = number
	}
	version := TargetVersion{Major: numbers[0], Minor: numbers[1]}
	if len(numbers) == 3 {
		version.Patch = numbers[2]
	}
	if version.Patch > 9999 {
		return TargetVersion{}, fmt.Errorf("%w: patch must be at most 9999", ErrInvalidTargetVersion)
	}
	if version.Major < 9 || version.Major == 9 && version.Minor < 1 {
		return TargetVersion{}, fmt.Errorf("%w: versions before 9.1 are unsupported", ErrInvalidTargetVersion)
	}
	if version.Major > 9 || version.Minor > 2 || version.Minor == 2 && version.Patch > 1015 {
		return TargetVersion{}, fmt.Errorf("%w: versions after %s are not described by this build", ErrInvalidTargetVersion, MaximumTargetVersion)
	}
	return version, nil
}

func targetVersionFromOptions(raw any) (TargetVersion, bool, string) {
	fallback, _ := ParseTargetVersion(DefaultTargetVersion)
	if raw == nil {
		return fallback, false, ""
	}
	var options map[string]any
	switch value := raw.(type) {
	case map[string]any:
		options = value
	case []byte:
		if len(value) == 0 || string(value) == "null" {
			return fallback, false, ""
		}
		if err := json.Unmarshal(value, &options); err != nil {
			return fallback, false, "vimls: initializationOptions must be an object; using target 9.1.0000"
		}
	default:
		return fallback, false, "vimls: initializationOptions must be an object; using target 9.1.0000"
	}
	target, exists := options["targetVersion"]
	if !exists || target == nil {
		return fallback, false, ""
	}
	value, ok := target.(string)
	if !ok {
		return fallback, false, "vimls: targetVersion must be a string; using target 9.1.0000"
	}
	version, err := ParseTargetVersion(value)
	if err != nil {
		return fallback, false, fmt.Sprintf("vimls: %v; using target 9.1.0000", err)
	}
	return version, true, ""
}

func runtimepathFromOptions(raw any) ([]string, bool, string) {
	if raw == nil {
		return nil, false, ""
	}
	var options map[string]any
	switch value := raw.(type) {
	case map[string]any:
		options = value
	case []byte:
		if len(value) == 0 || string(value) == "null" {
			return nil, false, ""
		}
		if err := json.Unmarshal(value, &options); err != nil {
			return nil, false, "vimls: initializationOptions must be an object; ignoring runtimepath"
		}
	default:
		return nil, false, "vimls: initializationOptions must be an object; ignoring runtimepath"
	}
	rawPaths, exists := options["runtimepath"]
	if !exists || rawPaths == nil {
		return nil, false, ""
	}
	var paths []string
	switch values := rawPaths.(type) {
	case []string:
		paths = append(paths, values...)
	case []any:
		paths = make([]string, 0, len(values))
		for _, rawPath := range values {
			path, ok := rawPath.(string)
			if !ok {
				return nil, true, "vimls: runtimepath must be an array of strings; ignoring runtimepath"
			}
			paths = append(paths, path)
		}
	default:
		return nil, true, "vimls: runtimepath must be an array of strings; ignoring runtimepath"
	}
	return normalizeRuntimePaths(paths), true, ""
}

const defaultRuntimepathTimeout = 2 * time.Second

func discoverDefaultRuntimePaths(parent context.Context) ([]string, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, defaultRuntimepathTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "vim",
		"--clean", "-u", "NONE", "-U", "NONE", "-i", "NONE",
		"--noplugin", "-n", "-es", "-X", "--not-a-term",
		"-c", "0put =json_encode(globpath(&runtimepath, '', 0, 1))",
		"-c", "1print",
		"-c", "qa!",
	)
	command.Env = cleanVimEnvironment(os.Environ())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return runtimepathEnvironmentFallback(fmt.Errorf("query installed Vim runtimepath: %w%s", err, vimStderrDetail(stderr.String())))
	}
	paths, unmarshalErr := runtimeDirectoriesFromVimOutput(stdout.Bytes())
	if unmarshalErr != nil {
		return runtimepathEnvironmentFallback(fmt.Errorf("decode installed Vim runtimepath: %w", unmarshalErr))
	}
	if len(paths) == 0 {
		return runtimepathEnvironmentFallback(errors.New("installed Vim returned no existing runtime directories"))
	}
	return paths, nil
}

func runtimeDirectoriesFromVimOutput(output []byte) ([]string, error) {
	output = bytes.TrimSpace(output)
	if len(output) == 0 || output[0] != '[' {
		return nil, errors.New("Vim runtimepath output is not a JSON array")
	}
	var paths []string
	if err := json.Unmarshal(output, &paths); err != nil {
		return nil, err
	}
	return existingRuntimeDirectories(paths), nil
}

func cleanVimEnvironment(environment []string) []string {
	blocked := map[string]bool{
		"EXINIT": true, "GVIMINIT": true, "LANG": true, "LC_ALL": true,
		"VIM": true, "VIMINIT": true, "VIMRUNTIME": true, "XDG_CONFIG_HOME": true,
	}
	result := make([]string, 0, len(environment)+2)
	for _, item := range environment {
		name := item
		if index := strings.IndexByte(item, '='); index >= 0 {
			name = item[:index]
		}
		if blocked[strings.ToUpper(name)] {
			continue
		}
		result = append(result, item)
	}
	return append(result, "LC_ALL=C", "LANG=C")
}

func existingRuntimeDirectories(paths []string) []string {
	paths = normalizeRuntimePaths(paths)
	result := paths[:0]
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			result = append(result, path)
		}
	}
	return result
}

func runtimepathEnvironmentFallback(cause error) ([]string, error) {
	path := strings.TrimSpace(os.Getenv("VIMRUNTIME"))
	if path == "" {
		return nil, cause
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, cause
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return nil, cause
	}
	return []string{filepath.Clean(absolute)}, cause
}

func vimStderrDetail(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 512 {
		value = value[:512]
	}
	return ": " + value
}

func targetVersionFromSettings(raw []byte, previous TargetVersion) (TargetVersion, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return previous, ""
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return previous, "vimls: workspace settings must be an object; retaining target " + previous.String()
	}
	value, exists := settings["targetVersion"]
	if nested, ok := settings["vimls"].(map[string]any); ok {
		if nestedValue, nestedExists := nested["targetVersion"]; nestedExists {
			value, exists = nestedValue, true
		}
	}
	if !exists {
		return previous, ""
	}
	text, ok := value.(string)
	if !ok {
		return previous, "vimls: targetVersion must be a string; retaining target " + previous.String()
	}
	version, err := ParseTargetVersion(text)
	if err != nil {
		return previous, fmt.Sprintf("vimls: %v; retaining target %s", err, previous.String())
	}
	return version, ""
}
