package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

//go:embed gitignore/token.txt
var token string
var debug bool = os.ExpandEnv("$DEBUG") == "1"

var yearRegexp = regexp.MustCompile(`(19\d\d|20\d\d)`)

func extractYear(filename string) string {
	return yearRegexp.FindString(filename)
}

func extractTitle(filename string, year string) string {
	title, _, found := strings.Cut(filename, year)
	if title == "" || !found {
		return filename
	}
	return strings.Trim(strings.ToLower(title), "() .")
}

func normalizeTitle(title string) string {
	runes := []rune{}
	for _, r := range title {
		if r == ' ' || r == '.' {
			runes = append(runes, '.')
		} else if r == '&' {
			runes = append(runes, 'a', 'n', 'd')
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			runes = append(runes, r)
		}
	}
	return string(runes)
}

func input() string {
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadBytes('\n')
	return strings.TrimRight(string(line), "\n")
}

func lookup(title, year string, what string) (candidates []string, err error) {
	purl, _ := url.Parse(fmt.Sprintf("https://api.themoviedb.org/3/search/%s", what))
	query := purl.Query()
	query.Add("query", url.QueryEscape(strings.ReplaceAll(title, ".", " ")))
	query.Add("language", "en")
	query.Add("page", "1")
	query.Add("include_adult", "false")
	purl.RawQuery = query.Encode()

	// fmt.Printf("url=%q\n", purl.String())

	request, err := http.NewRequest("GET", purl.String(), nil)
	if err != nil {
		return candidates, err
	}
	request.Header.Add("Accept", "application/json")
	request.Header.Add("Authorization", "Bearer "+strings.TrimRight(token, "\n"))
	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		return candidates, err
	}
	defer response.Body.Close()

	var parsedResponse struct {
		Results []struct {
			Title        string `json:"title"`
			OriginalName string `json:"original_name"`
			ReleaseDate  string `json:"release_date"`
			FirstAirDate string `json:"first_air_date"`
		}
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return candidates, err
	}

	if debug {
		fmt.Println(string(data))
	}

	if err := json.Unmarshal(data, &parsedResponse); err != nil {
		return candidates, err
	}

	for _, r := range parsedResponse.Results {
		var c string
		if what == "tv" && r.OriginalName != "" && r.FirstAirDate != "" {
			c = fmt.Sprintf("%s_%s", normalizeTitle(r.OriginalName), r.FirstAirDate[:4])	
		} else if r.Title != "" && r.ReleaseDate != "" {
			c = fmt.Sprintf("%s_%s", normalizeTitle(r.Title), r.ReleaseDate[:4])	
		}
		if c != "" {
			candidates = append(candidates, c)
		}
	}

	return candidates, nil
}

func main() {
	what := flag.String("what", "movie", "What section of the TMDB to query")
	flag.Parse()

	if len(flag.Args()) != 1 {
		fmt.Printf("usage: %s /path/to/movie.mp4", os.Args[0])
		os.Exit(1)
	}

	path := strings.TrimRight(flag.Args()[0], "/")
	ext := filepath.Ext(path)
	filename := filepath.Base(path)
	filename = filename[:len(filename) - len(ext)]

	year := extractYear(filename)
	title := extractTitle(filename, year)
	fmt.Printf("filename=%q\n", filename)

	fmt.Printf("year=%q.  Press Enter to confirm, or input correct year: ", year)
	inputYear := input()
	if inputYear != "" {
		year = inputYear
	}

	fmt.Printf("title=%q.  Press Enter to confirm, or input correct title: ", title)
	inputTitle := input()
	if inputTitle != "" {
		title = inputTitle
	}

	// fmt.Printf("filename=%q\n\ttitle=%q\n\tyear=%q\n", filename, title, year)
	candidates, err := lookup(title, year, *what)
	if err != nil {
		panic(err)
	}

	newTitle := ""
	for {
		fmt.Printf("candidates:\n")
		for i, c := range candidates {
			if i >= 10 {
				break
			}
			fmt.Printf("[ %2d ] %s\n", i+1, c)
		}
		fmt.Printf("what is the best candidate?  Leave blank to abort ")

		bestStr := input()
		if bestStr == "" {
			fmt.Println("aborted")
			os.Exit(1)
		}

		bestIdx, err := strconv.Atoi(bestStr)
		if err != nil || bestIdx == 0 || bestIdx > len(candidates) {
			continue
		}

		newTitle = candidates[bestIdx - 1]
		break
	}

	newPath := newTitle

	fi, err := os.Stat(path)
	if err != nil {
		panic(err)
	}
	if !fi.IsDir() {
		newPath += ext
	}

	for {
		fmt.Printf("rename %q to %q? [yes] / no ", path, newPath)
		switch strings.ToLower(input()) {
		case "", "y", "yes":
			if fi.IsDir() {
				//
				// Rename directory contents, e.g. subtitles, nfo files that
				// match the original name of the directory (prior to us
				// renaming it).
				//
				contents, err := os.ReadDir(path)
				if err != nil {
					panic(err)
				}
				for _, entry := range contents {
					if strings.HasPrefix(entry.Name(), path) {
						newName := strings.ReplaceAll(entry.Name(), path, newTitle)
						newPath := filepath.Join(path, newName)
						oldPath := filepath.Join(path, entry.Name())
						if err := os.Rename(oldPath, newPath); err != nil {
							panic(err)
						}
					}
				}
			}
			if err := os.Rename(path, newPath); err != nil {
				panic(err)
			}
			os.Exit(0)
		case "n", "no":
			fmt.Println("aborted")
			os.Exit(1)
		}
	}
}
