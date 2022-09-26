package koshka

// [x] Read config file for credentials, etc.
// [x] List S3 objects matching a given prefix
// [x] Stream a specific S3 object
// [x] Integrate with autocompletion
// [ ] Support for S3 versions
// [x] Support for aliases
// [.] Handle HTTP/S
// [ ] Handle local files
// [ ] Any other backends?
// [.] Tests!!
// [ ] GNU cat-compatible command-line flags
// [ ] Proper packaging
// [ ] CI to build binaries for MacOS, Windows and Linux

// [x] Where's the AWS SDK golang reference?  https://pkg.go.dev/github.com/aws/aws-sdk-go-v2
// [ ] How to package this thing without having to build separate binaries for kot, kedit, etc?

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

func Suggest(prefix string) (candidates []string, err error) {
	//
	// not sure why this bullshittery with prependScheme is necessary, but without
	// it the alias completion use case ends up missing the scheme
	//
	prependScheme := false
	sections, err := LoadConfig("")
	if err != nil {
		return []string{}, err
	}
	for _, section := range sections {
		if strings.HasPrefix(section.items["alias"], prefix) {
			prefix = section.name
			prependScheme = true
			break
		}
	}

	parsedUrl, err := url.Parse(prefix)
	if err != nil {
		return []string{}, err
	}

	//
	// TODO: parse HTTP directory listings for autocompletion
	//
	if parsedUrl.Scheme == "s3" {
		candidates, err = s3_list(prefix)
	} else {
		return []string{}, errors.New(fmt.Sprintf("unsupported scheme: %s", parsedUrl.Scheme))
	}

	if err != nil {
		return []string{}, err
	}
	if prependScheme {
		for i := range candidates {
			candidates[i] = fmt.Sprintf("%s:%s", parsedUrl.Scheme, candidates[i])
		}
	}
	return candidates, nil
}

func Cat(rawUrl string) error {
	if rawUrl == "-" {
		_, err := io.Copy(os.Stdout, os.Stdin)
		return err
	}
	parsedUrl, err := url.Parse(rawUrl)
	if err != nil {
		return err
	}
	switch parsedUrl.Scheme {
	case "s3":
		return s3_cat(rawUrl)
	case "http":
	case "https":
		return http_cat(rawUrl)
	}
	return fmt.Errorf("cat functionality for scheme %s not implemented yet", parsedUrl.Scheme)
}
