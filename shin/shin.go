//
// SSH into an AWS EC2 instance by its instance name
//
// TODO:
//
// - [ ] handle different AWS config profiles
// - [ ] handle EC2 instance tag names
// - [ ] update local ~/.ssh/config with instance info so you can just directly SSH into it
// - [ ] parameterize username (e.g. don't hardcode "ubuntu")
//
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

func ipAddressFromIID(instanceId string) string {
	ctx := context.TODO()
	config, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}
	client := ec2.NewFromConfig(config)
	params := ec2.DescribeInstancesInput{InstanceIds: []string{instanceId}}
	output, err := client.DescribeInstances(ctx, &params)
	if err != nil {
		log.Fatal(err)
	}
	for _, reservation := range output.Reservations {
		for _, instance := range reservation.Instances {
			return *instance.PublicIpAddress
		}
	}
	return ""
}

func ssh(ip string) error {
	userAtHost := fmt.Sprintf("ubuntu@%s", ip)
	identityFilePath := os.ExpandEnv("$IDENTITY_FILE_PATH")
	command := exec.Command(
		"ssh",
		userAtHost,
		"-i",
		identityFilePath,
        "-o", "StrictHostKeyChecking=no",
        "-o", "UserKnownHostsFile=/dev/null",
    )
	command.Stdout = os.Stdout
	command.Stdin = os.Stdin
	command.Stderr = os.Stderr
	err := command.Run()
	return err
}

func main() {
	ip := ipAddressFromIID(os.Args[1])
	err := ssh(ip)
	if err != nil {
		log.Fatal(err)
	}
}
