package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/atotto/clipboard"
)

//
// Read the contents of path, and write them to the clipboard
//
func copy(path string) {
	var (
		data []byte
		text string
		err error
	)
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		panic(err)
	}

	text = string(data)
	err = clipboard.WriteAll(text)
	if err != nil {
		panic(err)
	}
	fmt.Printf(text)
}

//
// Read contents from the clipboard, and save them to the path
//
func paste(path string) {
	var (
		fout *os.File
		text string
		err error
	)
	text, err = clipboard.ReadAll()
	if err != nil {
		panic(err)
	}

	if path == "-" {
		fout = os.Stdout
	} else {
		fout, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o660)
		if err != nil {
			panic(err)
		}
		defer fout.Close()
	}
	_, err = fout.Write([]byte(text))
	if err != nil {
		panic(err)
	}
}

//
// Edit the contents of the file using $EDITOR.
//
func edit(path string) {
	editor := os.ExpandEnv("$EDITOR")
	if editor == "$EDITOR" {
		editor = "vim"
	}

	command := exec.Command(editor, path)
	command.Stdin = os.Stdin
	command.Stderr = os.Stderr
	command.Stdout = os.Stdout
	err := command.Run()
	if err != nil {
		panic(err)
	}
}

func printUsage() {
	fmt.Println(`kp works with the copy-paste buffer.

Usage:

	kp <command>

The commands are:

	copy	Read standard input, write to the copy-paste buffer
	edit	Edit the contents of the copy-paste buffer using $EDITOR
	paste	Write the contents of the copy-paste buffer to standard output
	tmux	Write the contents of the current tmux pane to the copy-paste buffer
`)
	os.Exit(1)
}

func main() {
	var args []string = os.Args

	if len(args) < 2 {
		copy("-")
		return
	}

	command := args[1]
	args = args[2:]

	switch command {
	case "copy":
		var path string
		switch len(args) {
		case 0:
			path = "-"
		case 1:
			path = args[0]
		default:
			printUsage()
			return
		}
		copy(path)
	case "paste":
		var path string
		switch len(args) {
		case 0:
			path = "-"
		case 1:
			path = args[0]
		default:
			printUsage()
			return
		}
		paste(path)
	case "edit":
		tempFile, err := ioutil.TempFile("", "")
		if err != nil {
			panic(err)
		}
		defer os.Remove(tempFile.Name())

		paste(tempFile.Name())
		edit(tempFile.Name())
		copy(tempFile.Name())
	case "tmux":
		tempFile, err := ioutil.TempFile("", "")
		if err != nil {
			panic(err)
		}
		defer os.Remove(tempFile.Name())

		command := exec.Command("tmux", "capture-pane", "-p")
		command.Stderr = os.Stderr
		command.Stdout = tempFile
		err = command.Run()
		if err != nil {
			panic(err)
		}
		tempFile.Close()

		edit(tempFile.Name())
		copy(tempFile.Name())
	default:
		fmt.Printf("unknown command: %s\n", command)
		printUsage()
	}
}
