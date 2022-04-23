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
			Login string `json:"login"`
		} `json:"user"`
		Repo struct {
			SSHUrl string `json:"ssh_url"`
		} `json:"repo"`
		Ref string `json:"ref"`
	} `json:"head"`
}

func tweakUrl(url string) string {
	if strings.Contains(url, "api.github.com") {
		return url
	}
	url = strings.Replace(url, "github.com", "api.github.com", 1)
	url = strings.Replace(url, "/pull/", "/pulls/", 1)
	return url
}

func get(url string) PullRequest {
	response, err := http.Get(url)
	defer response.Body.Close()
	if err != nil {
		log.Fatal(err)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatal(err)
	}
	
	var pullRequest PullRequest
	err = json.Unmarshal(body, &pullRequest)
	if err != nil {
		log.Fatal(err)
	}

	return pullRequest
}

func run(executable string, args ...string) string {
	cmd := exec.Command(executable, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		log.Fatalf("the command `%s %s` failed: %s", executable, args, err)
	}
	return strings.TrimRight(out.String(), "\n")
}

func main() {
	if len(os.Args) == 0 {
		log.Fatalf("usage: hijack http://github.com/repos/ORG/REPO/pulls/123")
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

	fmt.Printf("Hijack of %s (%s/%s) progress.\n", url, user, ref)
	fmt.Println("`git commit` your changes and then run `hijack land`")
}
