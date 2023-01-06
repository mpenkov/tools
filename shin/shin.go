//
// SSH into an AWS EC2 instance by its instance name
//
// Use the IDENTITY_FILE_PATH environment variable to specify the *.pem file to connect with.
//
// TODO:
//
// - [ ] handle different AWS config profiles
// - [x] handle EC2 instance tag names
// - [x] update local ~/.ssh/config with instance info so you can just directly SSH into it
// - [x] parameterize username (e.g. don't hardcode "ubuntu")
//
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func findInstance(instanceId string) (ec2types.Instance, error) {
	ctx := context.TODO()
	config, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}
	client := ec2.NewFromConfig(config)

	if strings.HasPrefix(instanceId, "i-") {
		params := ec2.DescribeInstancesInput{InstanceIds: []string{instanceId}}
		output, err := client.DescribeInstances(ctx, &params)
		if err == nil {
			if len(output.Reservations) == 1 && len(output.Reservations[0].Instances) == 1 {
				return output.Reservations[0].Instances[0], nil
			}
			err = fmt.Errorf("no info for instance %q, it may have been purged", instanceId)
			return ec2types.Instance{}, err
		}
		return ec2types.Instance{}, err
	}

	// Try lookup by name
	filter := ec2types.Filter{Name: aws.String("tag:Name"), Values: []string{instanceId}}
	params := ec2.DescribeInstancesInput{Filters: []ec2types.Filter{filter}}
	output, err := client.DescribeInstances(ctx, &params)
	if err == nil {
		if len(output.Reservations) == 1 && len(output.Reservations[0].Instances) == 1 {
			return output.Reservations[0].Instances[0], nil
		}
		err = fmt.Errorf("no info for instance named %q, it may have been purged", instanceId)
		return ec2types.Instance{}, err
	}

	return ec2types.Instance{}, err
}

func register(instance ec2types.Instance, alias, username string) error {
	defaultAlias := *instance.InstanceId
	for _, tag := range instance.Tags {
		if *tag.Key == "Name" {
			defaultAlias = *tag.Value
			break
		}
	}
	if alias == "" {
		alias = defaultAlias
	}

	identityFilePath := os.ExpandEnv("$IDENTITY_FILE_PATH")
	if len(identityFilePath) == 0 {
		return fmt.Errorf("bad IDENTITY_FILE_PATH: %q", identityFilePath)
	}

	var buf bytes.Buffer
	buf.WriteString("\n# <shin>\n")
	buf.WriteString(fmt.Sprintf("Host %s\n", alias))
	buf.WriteString(fmt.Sprintf("\tHostname %s\n", *instance.PublicIpAddress))
	buf.WriteString(fmt.Sprintf("\tUser %s\n", username))
	buf.WriteString(fmt.Sprintf("\tIdentityFile %s\n", identityFilePath))
	buf.WriteString("# </shin>\n")

	fmt.Fprintf(os.Stderr, buf.String())

	path := os.ExpandEnv("$HOME/.ssh/config")
	fout, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer fout.Close()

	_, err = fout.WriteString(buf.String())
	return err

}

func ssh(ip string, username string, writeKnownHosts bool) error {
	userAtHost := fmt.Sprintf("%s@%s", username, ip)

	identityFilePath := os.ExpandEnv("$IDENTITY_FILE_PATH")
	if len(identityFilePath) == 0 {
		return fmt.Errorf("bad IDENTITY_FILE_PATH: %q", identityFilePath)
	}

	params := []string{
		userAtHost,
		"-i",
		identityFilePath,
		"-o", "StrictHostKeyChecking=no",
	}
	if !writeKnownHosts {
		params = append(params, "-o", "UserKnownHostsFile=/dev/null")
	}

	command := exec.Command("ssh", params...)
	command.Stdout = os.Stdout
	command.Stdin = os.Stdin
	command.Stderr = os.Stderr
	err := command.Run()
	return err
}

var (
	doNotConnect    = flag.Bool("dnc", false, "do not actually connect to the instance")
	registerHost    = flag.Bool("register", false, "register the instance instance in ~/.ssh/config")
	writeKnownHosts = flag.Bool("known", false, "write the fingerprint to ~/.ssh/known_hosts during initial connection")
	registerAlias   = flag.String("alias", "", "override the alias to register")
	username        = flag.String("username", "ubuntu", "the username to use for the connection")
)

func main() {
	//
	// NB. When calling this program, the non-flag arguments MUST follow the flag arguments.
	// If we don't do this, the command-line argument parsing will not work correctly.
	//
	flag.Parse()

	instance, err := findInstance(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	} else if *instance.PublicIpAddress == "" {
		log.Fatalf("instance %q does not have a public IP", flag.Arg(0))
	}

	if *registerHost {
		err := register(instance, *registerAlias, *username)
		if err != nil {
			log.Fatalf("failed to register host: %s", err)
		}
	}

	if !*doNotConnect {
		err := ssh(*instance.PublicIpAddress, *username, *writeKnownHosts)
		if err != nil {
			log.Fatal(err)
		}
	}
}
