package main

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
	"flag"
	"fmt"
	"log"

	"github.com/mpenkov/tools/koshka"
	"github.com/posener/complete/v2"
)

type PredictorType int

func (mpt PredictorType) Predict(prefix string) (candidates []string) {
	result, err := koshka.Suggest(prefix)
	if err == nil {
		return result
	}
	return []string{}
}

func main() {
	var testFlag = flag.Bool("test", false, "test the predictor")

	var predictor PredictorType
	cmd := &complete.Command{Args: predictor}
	cmd.Complete("kot")

	flag.Parse()

	if *testFlag {
		for _, thing := range predictor.Predict(flag.Args()[0]) {
			fmt.Println(thing)
		}
		return
	}

	for _, thing := range flag.Args() {
		err := koshka.Cat(thing)
		if err != nil {
			log.Fatal(err)
		}
	}
}
