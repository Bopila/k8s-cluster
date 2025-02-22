package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var sshUser = "user"
var sshPassword = "password"

var hosts = map[string]string{
	"172.16.197.100": "ubuntu",
	"172.16.197.110": "control-plane",
	"172.16.197.120": "worker-node1",
	"172.16.197.130": "worker-node2",
	"172.16.197.140": "worker-node3",
}

func main() {
	fmt.Println("🔑 Generating SSH key...")
	generateSSHKey()

	// First, configure passwordless sudo
	fmt.Println("🛠 Configuring passwordless sudo...")
	configurePasswordlessSudo()

	// After passwordless sudo is configured, configure /etc/hosts
	fmt.Println("📄 Configuring /etc/hosts...")
	configureHosts()

	// Enable SSH keys on all nodes
	fmt.Println("🔁 Enabling passwordless SSH...")
	setupSSH()
}

func generateSSHKey() {
	if _, err := os.Stat("/home/user/.ssh/id_rsa"); os.IsNotExist(err) {
		cmd := exec.Command("ssh-keygen", "-t", "rsa", "-b", "4096", "-N", "", "-f", "/home/user/.ssh/id_rsa")
		runCommand(cmd, "SSH key generation failed")
	} else {
		fmt.Println("🔑 SSH key already exists, skipping generation.")
	}
}

func configurePasswordlessSudo() {
	for ip := range hosts {
		// Modify the sudoers file to allow passwordless sudo
		cmd := exec.Command("sshpass", "-p", sshPassword, "ssh", fmt.Sprintf("%s@%s", sshUser, ip), "bash", "-c",
			fmt.Sprintf("echo '%s ALL=(ALL) NOPASSWD: ALL' | sudo -S tee -a /etc/sudoers", sshUser))
		runCommand(cmd, fmt.Sprintf("Failed to configure passwordless sudo on %s", ip))
	}
}

func configureHosts() {
	var hostsContent strings.Builder
	for ip, name := range hosts {
		hostsContent.WriteString(fmt.Sprintf("%s %s\n", ip, name))
	}

	for ip := range hosts {
		// This assumes passwordless sudo is configured for 'user'
		cmd := exec.Command("ssh", fmt.Sprintf("%s@%s", sshUser, ip), "bash", "-c", fmt.Sprintf("echo \"%s\" | sudo -S tee -a /etc/hosts", hostsContent.String()))
		runCommand(cmd, fmt.Sprintf("Failed to configure /etc/hosts on %s", ip))
	}
}

func setupSSH() {
	for ip := range hosts {
		fmt.Printf("🚀 Setting up %s...\n", ip)

		// Copy SSH key to the target host
		cmd := exec.Command("sshpass", "-p", sshPassword, "ssh-copy-id", "-o", "StrictHostKeyChecking=no", fmt.Sprintf("%s@%s", sshUser, ip))
		runCommand(cmd, fmt.Sprintf("Failed to copy SSH key to %s", ip))

		// Enable SSH between all nodes
		for subIP := range hosts {
			if ip != subIP {
				cmd := exec.Command("ssh", fmt.Sprintf("%s@%s", sshUser, ip), "sshpass", "-p", sshPassword, "ssh-copy-id", "-o", "StrictHostKeyChecking=no", fmt.Sprintf("%s@%s", sshUser, subIP))
				runCommand(cmd, fmt.Sprintf("Failed to enable SSH between %s and %s", ip, subIP))
			}
		}
	}
}

func runCommand(cmd *exec.Cmd, errorMessage string) {
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("❌ %s: %s\n", errorMessage, string(output))
		os.Exit(1)
	}
	fmt.Println(string(output))
}

