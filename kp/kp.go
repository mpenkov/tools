package main

//
// kp works with the copy-paste buffer
//
// read stuff into the buffer (copy):
//
//		cat file.txt | grep foo | kp copy -
//		kp copy file.txt
//
// edit the contents of the buffer (using $EDITOR):
//
//		kp edit
//
// write the contents of the buffer to stdout (like a paste):
//
//		kp paste
//

import (
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
	data := read(path)
	clipboard.WriteAll(data)
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

func main() {
	var (
		nargs int = len(os.Args)
	)

	if nargs == 1 {
		copy("-")
		return
	}

	command := os.Args[1]

	if command == "copy" {
		if nargs <= 2 {
			copy("-")
		} else if nargs == 3 {
			copy(os.Args[2])
		} else {
			printUsage()
		}
	} else if command == "paste" {
		if nargs == 2 {
			paste("-")
		} else if nargs == 3 {
			paste(os.Args[2])
		} else {
			printUsage()
		}
	} else if command == "edit" {
		tempDir, err := os.MkdirTemp("", "")
		if err != nil {
			panic(err)
		}
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
