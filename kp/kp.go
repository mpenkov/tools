package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/atotto/clipboard"
)

func read(path string) string {
	var (
		data []byte
		err error
	)
	
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			panic(err)
		}
	} else {
		fout, err := os.Open(path)
		if err != nil {
			panic(err)
		}
		defer fout.Close()
		data, err = io.ReadAll(fout)
		if err != nil {
			panic(err)
		}
	}
	return string(data)
}

func copy(path string) {
	var data string = read(path)
	clipboard.WriteAll(data)
	if *echo {
		fmt.Println(data)
	}
}

func paste(path string) {
	var (
		fout *os.File
		err error
	)
	if path == "-" {
		fout = os.Stdout
	} else {
		fout, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o660)
		if err != nil {
			panic(err)
		}
		defer fout.Close()
	}
	data, err := clipboard.ReadAll()
	if err != nil {
		panic(err)
	}
	_, err = fout.Write([]byte(data))
	if err != nil {
		panic(err)
	}
}

func printUsage() {
	fmt.Println("usage: kp [copy|paste|edit]")
	os.Exit(1)
}

var (
	echo = flag.Bool("echo", false, "echo the contents of the clipboard to stdout")
)

func main() {
	flag.Parse()

	var (
		args []string = flag.Args()
		nargs int = len(args)
	)

	if nargs == 0 {
		copy("-")
		return
	}

	command := args[0]

	if command == "copy" {
		if nargs <= 1 {
			copy("-")
		} else if nargs == 3 {
			copy(args[1])
		} else {
			printUsage()
		}
	} else if command == "paste" {
		if nargs == 1 {
			paste("-")
		} else if nargs == 2 {
			paste(args[1])
		} else {
			printUsage()
		}
	} else if command == "edit" {
		tempDir, err := os.MkdirTemp("", "")
		if err != nil {
			panic(err)
		}
		defer os.RemoveAll(tempDir)

		path := fmt.Sprintf("%s/clipboard.txt", tempDir)
		paste(path)

		editor := os.ExpandEnv("$EDITOR")
		if editor == "$EDITOR" {
			editor = "vim"
		}

		command := exec.Command(editor, path)
		command.Stdin = os.Stdin
		command.Stderr = os.Stderr
		command.Stdout = os.Stdout
		err = command.Run()
		if err != nil {
			panic(err)
		}

		copy(path)
	} else {
		printUsage()
	}
}
