package koshka

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

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
