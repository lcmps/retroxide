package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func LoadEnvironment(dotEnvPath string, process Lookup) (Config, error) {
	fileValues := make(map[string]string)
	if dotEnvPath != "" {
		file, err := os.Open(dotEnvPath)
		if err == nil {
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
					return Config{}, fmt.Errorf("invalid dotenv entry at line %d", lineNumber)
				}
				value, err := parseDotEnvValue(strings.TrimSpace(rawValue))
				if err != nil {
					return Config{}, fmt.Errorf("invalid dotenv value at line %d: %w", lineNumber, err)
				}
				fileValues[key] = value
			}
			if err := scanner.Err(); err != nil {
				return Config{}, fmt.Errorf("reading dotenv file: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("opening dotenv file: %w", err)
		}
	}
	lookup := func(key string) (string, bool) {
		if process != nil {
			if value, ok := process(key); ok {
				return value, true
			}
		}
		value, ok := fileValues[key]
		return value, ok
	}
	return Load(lookup)
}

func LoadProcessEnvironment() (Config, error) {
	return LoadEnvironment(".env", func(key string) (string, bool) {
		return os.LookupEnv(key)
	})
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
