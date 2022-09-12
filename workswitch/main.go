//
// Workspace switcher for i3.
//
// Each workspace keeps its own state.
//
package main

import (
	"flag"
	"fmt"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"strings"
)

var workspace = flag.Int("workspace", -1, "Switch to a workspace number")
var ibusEngine = flag.String("ibusengine", "", "Change the IBus engine for the current workspace")
var touchpadOff = flag.String("touchpadoff", "", "Change the TouchpadOff flag for the current workspace")

type state struct {
	IbusEngine string
	TouchpadOff string
}

var defaultState = state{IbusEngine: "xkb:jp::jpn", TouchpadOff: "0"}

func findJson(workspace int) string {
	return os.ExpandEnv(fmt.Sprintf("$HOME/.config/workswitch/%d.json", workspace))
}

func load(workspace int) (state, error) {
	path := findJson(workspace)
	f, err := os.Open(path)
	if err != nil {
		return state{}, err
	}
	defer f.Close()

	data := make([]byte, 1024768)
	numRead, err := f.Read(data)
	if err != nil || numRead == len(data) {
		return state{}, err
	}

	var state state
	err = json.Unmarshal(data[:numRead], &state)
	if err != nil {
		return state, fmt.Errorf("json decoding of %q failed: %w", path, err)
	}

	return state, nil
}

func save(workspace int, state state) error {
	path := findJson(workspace)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	_, err = f.Write(data)
	if err != nil {
		return err
	}

	return nil
}

func setIbusEngine(engine string) {
	fmt.Printf("setIbusEngine(%q)\n", engine)
	cmd := exec.Command("ibus", "engine", engine)
	if err := cmd.Run(); err != nil {
		log.Fatalf("'ibus engine %s' failed: %s", engine, err)
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
			return line[idx+2:idx+3]
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
	return value
}

func getCurrentWorkspace() int {
	cmd := exec.Command("i3-msg", "-t", "get_workspaces")
	stdout, err := cmd.Output()
	if err != nil {
		log.Fatalf("unable to get current workspace: %s", err)
	}
	type workspace struct {
		Num int
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

func main() {
	flag.Parse()

	if *workspace != -1 {
		cmd := exec.Command("i3-msg", "-t", "command", fmt.Sprintf("workspace number %d", *workspace))
		if err := cmd.Run(); err != nil {
			log.Fatal(err)
		}

		state, err := load(*workspace)
		if err != nil {
			log.Printf("could not load workspace %d: %s, using defaults", *workspace, err)
			state = defaultState
		}
		setIbusEngine(state.IbusEngine)
		setTouchpadOff(state.TouchpadOff)
	}

	if *ibusEngine != "" {
		setIbusEngine(*ibusEngine)

		currentWorkspace := getCurrentWorkspace()
		state, err := load(currentWorkspace)
		if err != nil {
			log.Printf("could not load workspace %d: %s, using defaults", *workspace, err)
			state = defaultState
		}
		state.IbusEngine = *ibusEngine
		save(currentWorkspace, state)
	}

	if *touchpadOff != "" {
		value := setTouchpadOff(*touchpadOff)

		currentWorkspace := getCurrentWorkspace()
		state, err := load(currentWorkspace)
		if err != nil {
			log.Printf("could not load workspace %d: %s, using defaults", *workspace, err)
			state = defaultState
		}
		state.TouchpadOff = value
		save(currentWorkspace, state)
	}
}
