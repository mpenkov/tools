//
// Workspace switcher for i3.
//
// Each workspace keeps its own state.
//
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var (
	workspace = flag.Int("workspace", -1, "Switch to a workspace number")
	touchpadOff = flag.String("touchpadoff", "", "Change the TouchpadOff flag for the current workspace")
	inputLanguage = flag.String("inputlanguage", "", "Set the input language for the currnet workspace")
	keyboardIndicator = flag.Bool("keyboardindicator", false, "Print the keyboard indicator to stdout")
	touchpadIndicator = flag.Bool("touchpadindicator", false, "Print the touchpad indicator to stdout")
)

type State struct {
	InputLanguage  string
	TouchpadOff    string
	MouseLocationX int
	MouseLocationY int

	Workspace int
}

func (s State) Save() error {
	path := findJson(s.Workspace)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	_, err = f.Write(data)
	if err != nil {
		return err
	}

	return nil
}

func (s State) Activate() {
	setInputLanguage(s.InputLanguage)
	setTouchpadOff(s.TouchpadOff)
	setMouseLocation(s.MouseLocationX, s.MouseLocationY)
}

func findJson(workspace int) string {
	return os.ExpandEnv(fmt.Sprintf("$HOME/.config/workswitch/%d.json", workspace))
}

func load(workspace int) (State, error) {
	path := findJson(workspace)
	f, err := os.Open(path)
	if err != nil {
		return State{}, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return State{}, err
	}

	var state State
	err = json.Unmarshal(data, &state)
	if err != nil {
		return state, fmt.Errorf("json decoding of %q failed: %w", path, err)
	}
	return state, nil
}

func setIbusEngine(engine string) {
	fmt.Printf("setIbusEngine(%q)\n", engine)
	cmd := exec.Command("ibus", "engine", engine)
	if err := cmd.Run(); err != nil {
		// log.Fatalf("'ibus engine %s' failed: %s", engine, err)
	}
}

func setInputLanguage(language string) {
	if language == "english" {
		setIbusEngine("xkb:jp::jpn")
		setKeyboardLayout("jp")
	} else if language == "russian" {
		setIbusEngine("xkb:jp::jpn")
		//
		// I've found that I need to us ru,jp (instead of just ru) in order to
		// get keyboard shortcuts (e.g. ctrl+C) to work with the Russian layout
		// enabled.  Otherwise, I run into this problem:
		//
		// https://bugzilla.mozilla.org/show_bug.cgi?id=69230
		//
		// for all applications, not just Firefox.
		//
		setKeyboardLayout("ru,jp")
	} else if language == "japanese" {
		setIbusEngine("mozc-jp")
		setKeyboardLayout("jp")
	} else {
		log.Fatalf("unsupported language: %q", language)
	}
}

func setKeyboardLayout(layout string) {
	fmt.Printf("setKeyboardLayout(%q)\n", layout)
	cmd := exec.Command("setxkbmap", layout)
	err := cmd.Run()
	if err != nil {
		log.Fatalf("setxkbmap failed: %s", err)
	}
}

func getTouchpadOff() string {
	cmd := exec.Command("synclient")
	stdoutBytes, err := cmd.Output()
	if err != nil {
		log.Fatalf("synclient failed: %s", err)
	}
	stdout := string(stdoutBytes)
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "TouchpadOff") {
			idx := strings.LastIndex(line, "= ")
			return line[idx+2 : idx+3]
		}
	}
	log.Fatalf("could not parse synclient output")
	return ""
}

func setTouchpadOff(value string) string {
	fmt.Printf("setTouchpadOff(%q)\n", value)
	if value == "toggle" {
		oldValue := getTouchpadOff()
		if oldValue == "0" {
			value = "1"
		} else {
			value = "0"
		}
	}
	cmd := exec.Command("synclient", fmt.Sprintf("TouchpadOff=%s", value))
	if err := cmd.Run(); err != nil {
		log.Fatalf("synclient failed: %s", err)
	}


	//
	// syndaemon gets confused when we tweak TouchpadOff ourselves, so we
	// restart it here.
	//
	exec.Command("killall", "syndaemon").Run()
	exec.Command("syndaemon", "-i", "2.0", "-d", "-t", "-K")

	return value
}

func getMouseLocation() (int, int) {
	cmd := exec.Command("xdotool", "getmouselocation")
	stdoutBytes, err := cmd.Output()
	if err != nil {
		log.Fatalf("xdotool getmouselocation failed: %s", err)
	}

	coords := []int{9999, 9999}
	patterns := []*regexp.Regexp{regexp.MustCompile(`x:\d+`), regexp.MustCompile(`y:\d+`)}

	for i := range patterns {
		location := patterns[i].FindIndex(stdoutBytes)
		if location != nil {
			start := location[0] + 2
			end := location[1]
			coords[i], _ = strconv.Atoi(string(stdoutBytes[start:end]))
		}
	}

	return coords[0], coords[1]
}

func setMouseLocation(xPos, yPos int) {
	fmt.Printf("setMouseLocation(%d, %d)\n", xPos, yPos)
	x := fmt.Sprintf("%d", xPos)
	y := fmt.Sprintf("%d", yPos)
	cmd := exec.Command("xdotool", "mousemove", x, y)
	if err := cmd.Run(); err != nil {
		log.Fatalf("xdotool mousemove failed: %s", err)
	}
}

func getCurrentWorkspace() int {
	cmd := exec.Command("i3-msg", "-t", "get_workspaces")
	stdout, err := cmd.Output()
	if err != nil {
		log.Fatalf("unable to get current workspace: %s", err)
	}
	type workspace struct {
		Num     int
		Focused bool
	}
	var result []workspace

	err = json.Unmarshal(stdout, &result)
	if err != nil {
		log.Fatalf("unable to decode output from i3-msg: %s", err)
	}

	for _, w := range result {
		if w.Focused {
			return w.Num
		}
	}

	log.Fatalf("no focused workspaces?!")
	return -1
}

func loadCurrentState() State {
	currentWorkspace := getCurrentWorkspace()
	state, err := load(currentWorkspace)
	if err != nil {
		log.Printf("could not load workspace %d: %s, using defaults", *workspace, err)
		state = State{
			Workspace: currentWorkspace,
			InputLanguage: "english",
		}
	}
	return state
}

func main() {
	flag.Parse()

	configDir := os.ExpandEnv("$HOME/.config/workswitch")
	os.MkdirAll(configDir, 0o770)

	if *workspace != -1 {
		//
		// Save the mouse location for the current workspace before switching
		// to the new workspace and loading its settings.
		//
		state := loadCurrentState()

		x, y := getMouseLocation()
		state.MouseLocationX = x
		state.MouseLocationY = y
		state.Save()

		cmd := exec.Command("i3-msg", "-t", "command", fmt.Sprintf("workspace number %d", *workspace))
		if err := cmd.Run(); err != nil {
			log.Fatal(err)
		}

		state, err := load(*workspace)
		if err != nil {
			log.Printf("could not load workspace %d: %s, using defaults", *workspace, err)
			state = State{
				Workspace: *workspace,
				InputLanguage: "english",
			}
		}

		state.Activate()
	}

	if *inputLanguage != "" {
		setInputLanguage(*inputLanguage)
		state := loadCurrentState()
		state.InputLanguage = *inputLanguage
		state.Save()
	}

	if *touchpadOff != "" {
		value := setTouchpadOff(*touchpadOff)
		state := loadCurrentState()
		state.TouchpadOff = value
		state.Save()
	}

	if *keyboardIndicator {
		state := loadCurrentState()
		switch state.InputLanguage {
		case "", "english":
			fmt.Println("🟢 QWERTY")
		case "russian":
			fmt.Println("🔵 ЙЦУКЕН")
			fmt.Println()
			fmt.Println("#ff0000")
		case "japanese":
			fmt.Println(" 🔴 日 本 語")
		default:
			log.Fatalf("unknown language: %q", state.InputLanguage)
		}
	}

	if *touchpadIndicator {
		//
		// syndaemon also changes this value, so we rely on it directly instead
		// of looking it up in the workspace state
		//
		value := getTouchpadOff()
		switch value {
		case "0":
			fmt.Println("🐾 ON")
		case "1":
			fmt.Println("🐾 OFF")
			fmt.Println()
			fmt.Println("#ff0000")
		case "2":
			fmt.Println("🐾 BLK")
			fmt.Println()
			fmt.Println("#ff6900")
		}
	}
}
