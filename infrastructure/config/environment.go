package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	ConfigFileKey     = "RETROXIDE_CONFIG_FILE"
	defaultConfigFile = ".env"
)

func ProcessLookup(key string) (string, bool) {
	return os.LookupEnv(key)
}

func FilePath(process Lookup) string {
	if process != nil {
		if path, ok := process(ConfigFileKey); ok && strings.TrimSpace(path) != "" {
			return path
		}
	}
	return defaultConfigFile
}

func ReadFile(path string) (map[string]string, error) {
	values := make(map[string]string)
	if path == "" {
		return values, nil
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return values, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening dotenv file: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, rawValue, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("invalid dotenv entry at line %d", lineNumber)
		}
		value, err := parseDotEnvValue(strings.TrimSpace(rawValue))
		if err != nil {
			return nil, fmt.Errorf("invalid dotenv value at line %d: %w", lineNumber, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading dotenv file: %w", err)
	}
	return values, nil
}

// WriteFile merges values into the dotenv file at path, replacing it atomically with owner-only permissions.
func WriteFile(path string, values map[string]string) error {
	return UpdateFile(path, values, nil)
}

// UpdateFile sets values and deletes the remove keys in the dotenv file at path, replacing it
// atomically with owner-only permissions.
func UpdateFile(path string, values map[string]string, remove []string) error {
	merged, err := ReadFile(path)
	if err != nil {
		return err
	}
	for key, value := range values {
		merged[key] = value
	}
	for _, key := range remove {
		delete(merged, key)
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var contents strings.Builder
	for _, key := range keys {
		contents.WriteString(key + "=" + strconv.Quote(merged[key]) + "\n")
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".retroxide-config-*")
	if err != nil {
		return fmt.Errorf("creating temporary config file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("securing config file: %w", err)
	}
	if _, err := temporary.WriteString(contents.String()); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("writing config file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing config file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replacing config file: %w", err)
	}
	return nil
}

// Layered gives non-empty process values precedence over file values.
func Layered(process Lookup, file map[string]string) Lookup {
	return func(key string) (string, bool) {
		if process != nil {
			if value, ok := process(key); ok && value != "" {
				return value, true
			}
		}
		if value, ok := file[key]; ok {
			return value, true
		}
		if process != nil {
			return process(key)
		}
		return "", false
	}
}

func LoadEnvironment(dotEnvPath string, process Lookup) (Config, error) {
	fileValues, err := ReadFile(dotEnvPath)
	if err != nil {
		return Config{}, err
	}
	return Load(Layered(process, fileValues))
}

func LoadProcessEnvironment() (Config, error) {
	return LoadEnvironment(FilePath(ProcessLookup), ProcessLookup)
}

func parseDotEnvValue(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if value[0] == '\'' || value[0] == '"' {
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", err
		}
		return parsed, nil
	}
	if index := strings.Index(value, " #"); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	return value, nil
}
