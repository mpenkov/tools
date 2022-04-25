package koshka

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)


//
// Load the relevant configuration from ~/kot.cfg
//
func findConfig(prefix string, path string) (map[string]string, error) {
	if path == "" {
		path = os.ExpandEnv("$HOME/kot.cfg")
	}

	fin, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fin.Close()
	reader := bufio.NewReader(fin)

	// open the config file
	// look for the first section that matches the prefix
	// will need to test this thing...
	section := make(map[string]string)
	is_inside := false
	for true {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		line = strings.Trim(line, "\n")

		if len(line) == 0 || line[0] == '#' {
			// Skip comments
			continue
		}

		if line[0] == '[' && line[len(line) - 1] == ']' {
			section_name := line[1:len(line) - 1]
			if is_inside {
				// End of the relevant section
				return section, nil
			}
			if strings.HasPrefix(prefix, section_name) {
				is_inside = true
			}
		} else if is_inside {
			parts := strings.Split(line, "=")
			if len(parts) != 2 {
				return nil, errors.New(fmt.Sprintf("malformed line: %q", line))
			}
			key := strings.Trim(parts[0], " ")
			value := strings.Trim(parts[1], " ")
			section[key] = value
		}
	}
	if is_inside {
		return section, nil
	}

	return nil, errors.New(fmt.Sprintf("no matches found for prefix: %q", prefix))
}

