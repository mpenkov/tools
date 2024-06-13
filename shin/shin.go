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
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func findInstance(instanceId string, region string, profile string) (ec2types.Instance, error) {
	ctx := context.TODO()
	config, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(region),
		config.WithSharedConfigProfile(profile),
	)
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

	var buf bytes.Buffer
	buf.WriteString("\n# <shin>\n")
	buf.WriteString(fmt.Sprintf("Host %s\n", alias))
	buf.WriteString(fmt.Sprintf("\tHostname %s\n", *instance.PublicIpAddress))
	buf.WriteString(fmt.Sprintf("\tUser %s\n", username))
	buf.WriteString(fmt.Sprintf("\tIdentityFile %s\n", *pemPath))
	buf.WriteString("\tStrictHostKeyChecking=accept-new\n")
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

func ssh(userAtHost string, writeKnownHosts bool, sshArgs []string) error {
	params := []string{
		userAtHost,
		"-i", *pemPath,
		"-o", "StrictHostKeyChecking=no",
	}
	if !writeKnownHosts {
		params = append(params, "-o", "UserKnownHostsFile=/dev/null")
	}
	params = append(params, sshArgs...)

	command := exec.Command("ssh", params...)
	command.Stdout = os.Stdout
	command.Stdin = os.Stdin
	command.Stderr = os.Stderr
	return command.Run()
}

func scp(src string, dst string, writeKnownHosts bool, sshArgs []string) error {
	params := []string{"-i", *pemPath, "-o", "StrictHostKeyChecking=no"}
	if !writeKnownHosts {
		params = append(params, "-o", "UserKnownHostsFile=/dev/null")
	}
	params = append(params, sshArgs...)
	params = append(params, src, dst)

	command := exec.Command("scp", params...)
	command.Stdout = os.Stdout
	command.Stdin = os.Stdin
	command.Stderr = os.Stderr
	return command.Run()
}

func resolve(host string, region string, profile string) (ip string, instance ec2types.Instance) {
	if net.ParseIP(host) != nil {
		return host, ec2types.Instance{}
	}

	instance, err := findInstance(host, region, profile)
	if err != nil {
		log.Fatal(err)
	} else if instance.State.Name != ec2types.InstanceStateNameRunning {
		log.Fatalf(
			"instance %s is currently %s, cannot SSH to it",
			*instance.InstanceId,
			instance.State.Name,
		)
	}

	if instance.Ipv6Address != nil {
		return *instance.Ipv6Address, instance
	} else if instance.PublicIpAddress != nil {
		return *instance.PublicIpAddress, instance
	}

	log.Fatalf("instance %s does not have an IP address", *instance.InstanceId)
	return "", ec2types.Instance{}
}

func resolvePath(path string, username string, region string, profile string) string {
	if !strings.Contains(path, ":") {
		return path
	}

	idx := strings.Index(path, ":")
	host, _ := resolve(path[:idx], region, profile)

	//
	// Surround IPv6 IPs in square brackets in order for SCP to treat
	// them correctly.
	//
	if strings.Count(host, ":") > 2 {
		host = fmt.Sprintf("[%s]", host)
	}

	return fmt.Sprintf("%s@%s:%s", username, host, path[idx+1:])
}

var (
	doNotConnect    = flag.Bool("dnc", false, "do not actually connect to the instance")
	registerHost    = flag.Bool("register", false, "register the instance instance in ~/.ssh/config")
	writeKnownHosts = flag.Bool("known", false, "write the fingerprint to ~/.ssh/known_hosts during initial connection")
	registerAlias   = flag.String("alias", "", "override the alias to register")
	username        = flag.String("username", "ubuntu", "the username to use for the connection")
	scpMode         = flag.Bool("scp", false, "behave like scp instead of ssh")
	region          = flag.String("region", "us-east-2", "the AWS region within which to work")
	profile         = flag.String("profile", "default", "the AWS profile to use")
	pemPath         = flag.String("pem", os.ExpandEnv("$IDENTITY_FILE_PATH"), "the identity file to use when connecting via SSH")
)

func main() {
	//
	// NB. When calling this program, the non-flag arguments MUST follow the flag arguments.
	// If we don't do this, the command-line argument parsing will not work correctly.
	//
	flag.Parse()

	if *scpMode {
		src := resolvePath(flag.Arg(0), *username, *region, *profile)
		dst := resolvePath(flag.Arg(1), *username, *region, *profile)
		sshArgs := flag.Args()[2:]
		scp(src, dst, *writeKnownHosts, sshArgs)
	} else {
		ip, instance := resolve(flag.Arg(0), *region, *profile)
		sshArgs := flag.Args()[1:]

		if *registerHost && instance.InstanceId != nil {
			err := register(instance, *registerAlias, *username)
			if err != nil {
				log.Fatalf("failed to register host: %s", err)
			}
		}

		if !*doNotConnect {
			dst := fmt.Sprintf("%s@%s", *username, ip)
			if err := ssh(dst, *writeKnownHosts, sshArgs); err != nil {
				log.Fatal(err)
			}
		}
	}
}
