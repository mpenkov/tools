package koshka

// [x] Read config file for credentials, etc.
// [x] List S3 objects matching a given prefix
// [x] Stream a specific S3 object
// [x] Integrate with autocompletion
// [ ] Support for S3 versions
// [ ] Support for aliases
// [ ] Handle HTTP/S
// [ ] Handle local files
// [ ] Any other backends?
// [.] Tests!!
// [ ] GNU cat-compatible command-line flags
// [ ] Proper packaging
// [ ] CI to build binaries for MacOS, Windows and Linux

// [x] Where's the AWS SDK golang reference?  https://pkg.go.dev/github.com/aws/aws-sdk-go-v2
// [ ] How to package this thing without having to build separate binaries for kot, kedit, etc?

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

func s3_split(rawUrl string) (bucket, key string) {
	parsedUrl, err := url.Parse(rawUrl)
	if err != nil {
		log.Fatal(err)
	}

	if parsedUrl.Scheme != "s3" {
		log.Fatalf("not an S3 url: %s", rawUrl)
	}

	bucket = parsedUrl.Host
	key = strings.TrimLeft(parsedUrl.Path, "/")
	return
}

func s3_configure(url string) (aws.Config, error) {
	kotConfig, err := findConfig(url, "")
	if err == nil {
		if endpointUrl, ok := kotConfig["endpoint_url"]; ok {
			// https://aws.github.io/aws-sdk-go-v2/docs/configuring-sdk/endpoints/
			customResolver := aws.EndpointResolverWithOptionsFunc(
				func(service, region string, options ...interface{}) (aws.Endpoint, error) {
					return aws.Endpoint{URL: endpointUrl, HostnameImmutable: true}, nil
				},
			)
			return config.LoadDefaultConfig(
				context.TODO(),
				config.WithEndpointResolverWithOptions(customResolver),
			)
		}
	}
	return config.LoadDefaultConfig(context.TODO())
}

func s3_cat(url string) error {
	bucket, key := s3_split(url)

	cfg, err := s3_configure(url)
	if err != nil {
		return fmt.Errorf("unable to load configuration for url %q: %w", url, err)
	}

	client := s3.NewFromConfig(cfg)
	params := &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
	response, err := client.GetObject(context.TODO(), params)
	if err != nil {
		return fmt.Errorf("unable to read from url %q: %w", url, err)
	}

	defer response.Body.Close()
	const bufsize = 1024768
	buffer := make([]byte, bufsize)
	for true {
		numBytes, err := response.Body.Read(buffer)
		if numBytes > 0 {
			fmt.Printf("%s", buffer[:numBytes])
		}
		if err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("unable to read stream from url %q: %w", url, err)
		}
	}

	return nil
}

func s3_list(prefix string) (candidates []string, err error) {
	if prefix == "" {
		return candidates, errors.New("unable to list empty prefix")
	}

	bucket, keyPrefix := s3_split(prefix)
	cfg, err := s3_configure(prefix)
	if err != nil {
		return candidates, fmt.Errorf("unable to load configuration for url %q: %w", prefix, err)
	}

	client := s3.NewFromConfig(cfg)

	//
	// Attempt bucket name autocompletion
	//
	if keyPrefix == "" {
		listBucketsParams := &s3.ListBucketsInput{}
		response, err := client.ListBuckets(context.TODO(), listBucketsParams)
		if err != nil {
			return candidates, fmt.Errorf("unable to ListBuckets: %w", err)
		}
		matchingBuckets := []string{}
		for _, b := range(response.Buckets) {
			if strings.HasPrefix(*b.Name, bucket) {
				matchingBuckets = append(matchingBuckets, *b.Name)
			}
		}

		if len(matchingBuckets) == 1 {
			bucket = matchingBuckets[0]
			keyPrefix = ""
		} else {
			for _, b := range matchingBuckets {
				candidates = append(candidates, fmt.Sprintf("//%s", b))
			}
			return candidates, nil
		}
	}

	//
	// Drill down as far as possible
	//
	for true {
		// log.Printf("prefix: %s", prefix)
		listObjectsParams := &s3.ListObjectsInput{
			Bucket: aws.String(bucket),
			Prefix: aws.String(keyPrefix),
			Delimiter: aws.String("/"),
		}

		response, err := client.ListObjects(context.TODO(), listObjectsParams)
		if err != nil {
			return candidates, fmt.Errorf(
				"unable to ListObjects for bucket %q prefix %q: %w",
				bucket,
				prefix,
				err,
			)
		}

		if len(response.CommonPrefixes) == 1 && len(response.Contents) == 0 {
			keyPrefix = *response.CommonPrefixes[0].Prefix
			continue
		}

		// TODO: pagination?  Is it really worth it?
		// FIXME: why _must_ we include the //bucket, but exclude the s3: part?
		// Is colon some sort of special character for the autocompletion engine?

		for _, cp := range response.CommonPrefixes {
			fullUrl := fmt.Sprintf("//%s/%s", bucket, *cp.Prefix)
			candidates = append(candidates, fullUrl)
		}

		for _, obj := range response.Contents {
			fullUrl := fmt.Sprintf("//%s/%s", bucket, *obj.Key)
			candidates = append(candidates, fullUrl)
		}

		break
	}

	return candidates, nil
}

func Suggest(prefix string) (candidates []string, err error) {
	// TODO: alias completion goes here
	
	parsedUrl, err := url.Parse(prefix)
	if err != nil {
		return []string{}, err
	}
	if parsedUrl.Scheme == "s3" {
		return s3_list(prefix)
	}
	return []string{}, errors.New(fmt.Sprintf("unsupported scheme: %s", parsedUrl.Scheme))
}

func Cat(rawUrl string) error {
	parsedUrl, err := url.Parse(rawUrl)
	if err != nil {
		return err
	}
	if parsedUrl.Scheme == "s3" {
		return s3_cat(rawUrl)
	}
	return fmt.Errorf("cat functionality for scheme %s not implemented yet", parsedUrl.Scheme)
}
