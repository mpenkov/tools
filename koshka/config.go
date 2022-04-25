package koshka

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type CfgSection struct {
	name string
	items map[string]string
}

func LoadConfig(path string) ([]CfgSection, error) {
	if path == "" {
		path = os.ExpandEnv("$HOME/kot.cfg")
	}

	var result []CfgSection

	fin, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer fin.Close()
	reader := bufio.NewReader(fin)

	var currentSection CfgSection
	outputSection := false

	for true {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return result, err
		}

		line = strings.Trim(line, "\n")

		if len(line) == 0 || line[0] == '#' {
			// Skip comments
			continue
		}

		if line[0] == '[' && line[len(line) - 1] == ']' {
			if outputSection {
				result = append(result, currentSection)
			}
			currentSection.name = line[1:len(line) - 1]
			currentSection.items = make(map[string]string)
			outputSection = true
		} else {
			parts := strings.Split(line, "=")
			if len(parts) != 2 {
				return result, errors.New(fmt.Sprintf("malformed line: %q", line))
			}
			key := strings.Trim(parts[0], " ")
			value := strings.Trim(parts[1], " ")
			currentSection.items[key] = value
			outputSection = true
		}
	}
	if outputSection {
		result = append(result, currentSection)	
	}

	return result, nil
}

//
// Load the relevant configuration from ~/kot.cfg
//
func findConfig(prefix string, path string) (map[string]string, error) {
	if path == "" {
		path = os.ExpandEnv("$HOME/kot.cfg")
	}

	sections, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}

	for _, section := range(sections) {
		if strings.HasPrefix(prefix, section.name) || section.items["alias"] == prefix {
			return section.items, nil
		}
	}
	return nil, errors.New(fmt.Sprintf("no matches found for prefix: %q", prefix))
}

