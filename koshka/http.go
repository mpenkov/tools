package koshka

import (
	_ "fmt"
	"io"
	"net/http"
	"os"
)

func http_cat(rawUrl string) error {
	response, err := http.Get(rawUrl)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	io.Copy(os.Stdout, response.Body)
	return nil
}
