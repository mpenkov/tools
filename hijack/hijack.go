//
// Hijack a PR for editing locally
//
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
)


type PullRequest struct {
	Head struct {
		User struct {
			Login string
		}
		Repo struct {
			SSHUrl string `json:"ssh_url"`
		}
		Ref string
	}
}

func tweakUrl(url string) string {
	if strings.Contains(url, "api.github.com") {
		return url
	}
	url = strings.Replace(url, "github.com/", "api.github.com/repos/", 1)
	url = strings.Replace(url, "/pull/", "/pulls/", 1)
	return url
}

func get(url string) PullRequest {
	// log.Printf("get(%q)", url)
	response, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatal(err)
	}

	// fmt.Println(string(body))
	
	var pr PullRequest
	err = json.Unmarshal(body, &pr)
	if err != nil {
		log.Fatal(err)
	}

	if pr.Head.User.Login == "" || pr.Head.Repo.SSHUrl == "" || pr.Head.Ref == "" {
		log.Fatalf("bad PullRequest: %s, JSON unmarshalling appears to have failed", pr)
	}

	return pr
}

func run(executable string, args ...string) string {
	cmd := exec.Command(executable, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		//
		// TODO: print command output to standard error
		//
		log.Fatalf("the command `%s %s` failed: %s", executable, args, err)
	}
	return strings.TrimRight(out.String(), "\n")
}

func main() {
	if len(os.Args) == 1 {
		fmt.Printf("usage: hijack http://github.com/repos/ORG/REPO/pulls/123\n")
		os.Exit(1)
	}

	currentBranch := run("git", "branch", "--show-current")
	out := run("git", "rev-parse", "--abbrev-ref", "HEAD@{upstream}")
	split := strings.Split(out, "/")
	remote := split[0]
	remoteBranch := split[1]

	if os.Args[1] == "land" {
		run("git", "push", remote, fmt.Sprintf("%s:%s", currentBranch, remoteBranch))
		fmt.Println("MISSION ACCOMPLISHED!!")
		return
	} else if os.Args[1] == "cleanup" {
		//
		// Cleanup to prevent remotes and branches from piling up
		//
		run("git", "checkout", "HEAD", "--detach")
		run("git", "branch", "--delete", currentBranch)
		run("git", "remote", "remove", remote)
		fmt.Println("cleanup complete")
		return
	}

	// TODO: check if a hijack is already in progress

	url := tweakUrl(os.Args[1])
	pullRequest := get(url)
	var user string = pullRequest.Head.User.Login
	var ref string = pullRequest.Head.Ref
	
	//
	// If we already have a remote for the user, avoid adding it, because that
	// will cause git to error out and we don't want that here.
	//
	stdout := run("git", "remote")
	if !strings.Contains(stdout, user) {
		run("git", "remote", "add", user, pullRequest.Head.Repo.SSHUrl)
	}
	run("git", "fetch", user)

	var upstream string = fmt.Sprintf("%s/%s", user, ref)
	run("git", "checkout", upstream)
	//
	// Prefix the local branch name with the user to avoid naming clashes with
	// common existing branches, e.g. develop
	//
	run("git", "switch", "-c", fmt.Sprintf("%s_%s", user, ref))

	//
	// Set the upstream so we can push back to it more easily
	//
	run("git", "branch", "--set-upstream-to", upstream)

	fmt.Printf("Hijack of %q (%s/%s) in progress.\n", url, user, ref)
	fmt.Println("`git commit` your changes and then run `hijack land`")
	fmt.Println("to remove all traces of this tool, run `hijack cleanup`")
}
