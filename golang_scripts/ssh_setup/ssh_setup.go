package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/joho/godotenv"
)

var sshUser string
var sshPassword string
var hosts = make(map[string]string)

func main() {
	fmt.Println("🔐 Retrieving secure SSH credentials...")
	sshUser, sshPassword = GetSSHCredentials()

	fmt.Println("📂 Loading hosts from file...")
	loadHostsFromFile("servers.txt")

	fmt.Println("🔑 Generating SSH key...")
	generateSSHKey()

	fmt.Println("🛠 Configuring passwordless sudo...")
	configurePasswordlessSudo()

	fmt.Println("📄 Configuring /etc/hosts...")
	configureHosts()

	fmt.Println("🔁 Enabling passwordless SSH...")
	setupSSH()
}

// GetSSHCredentials loads the SSH username and password securely
func GetSSHCredentials() (string, string) {
	// Load environment variables from a .env file if present
	err := godotenv.Load(".env")
	if err != nil {
		log.Println("⚠️ No .env file found, using system environment variables.")
	}

	sshUser := os.Getenv("SSH_USER")
	sshPassword := os.Getenv("SSH_PASSWORD")

	if sshUser == "" || sshPassword == "" {
		log.Fatal("❌ SSH_USER and SSH_PASSWORD must be set as environment variables or in a .env file")
	}

	return sshUser, sshPassword
}

// Load hosts from the servers.txt file
func loadHostsFromFile(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			ip := parts[0]
			hostname := strings.Split(parts[1], ",")[0]
			hosts[ip] = hostname
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

// Generate SSH key if not present
func generateSSHKey() {
	for ip := range hosts {
		fmt.Printf("🔑 Checking SSH key on %s...\n", ip)
		cmdCheckKey := exec.Command("sshpass", "-p", sshPassword,
			"ssh", "-o", "StrictHostKeyChecking=no", "-tt",
			fmt.Sprintf("%s@%s", sshUser, ip),
			"if [ -f /home/user/.ssh/id_rsa ]; then echo 'exists'; fi")
		output, _ := cmdCheckKey.CombinedOutput()

		if strings.Contains(string(output), "exists") {
			fmt.Println("🔑 SSH key already exists, skipping generation.")
		} else {
			cmd := exec.Command("sshpass", "-p", sshPassword,
				"ssh", "-o", "StrictHostKeyChecking=no", "-tt",
				fmt.Sprintf("%s@%s", sshUser, ip),
				"ssh-keygen -t rsa -b 4096 -N '' -f /home/user/.ssh/id_rsa")
			runCommand(cmd, fmt.Sprintf("Failed to generate SSH key on %s", ip))
		}
	}
}

// Configure passwordless sudo if not already configured
func configurePasswordlessSudo() {
	for ip := range hosts {
		fmt.Printf("🛠 Checking passwordless sudo on %s...\n", ip)

		cmdSudoCheck := exec.Command("sshpass", "-p", sshPassword,
			"ssh", "-o", "StrictHostKeyChecking=no", "-tt",
			fmt.Sprintf("%s@%s", sshUser, ip),
			fmt.Sprintf("sudo -l | grep -q '(ALL) NOPASSWD: ALL' && echo 'passwordless'", sshUser))
		output, _ := cmdSudoCheck.CombinedOutput()

		if strings.Contains(string(output), "passwordless") {
			fmt.Println("🛠 Passwordless sudo is already configured, skipping.")
		} else {
			cmdSudo := exec.Command("sshpass", "-p", sshPassword,
				"ssh", "-o", "StrictHostKeyChecking=no", "-tt",
				fmt.Sprintf("%s@%s", sshUser, ip),
				fmt.Sprintf("echo \"%s ALL=(ALL) NOPASSWD: ALL\" | sudo -S tee /etc/sudoers.d/%s && sudo chmod 440 /etc/sudoers.d/%s", sshUser, sshUser, sshUser))
			runCommand(cmdSudo, fmt.Sprintf("Failed to configure passwordless sudo on %s", ip))
		}
	}
}

// Configure /etc/hosts if not already configured
func configureHosts() {
	var hostsContent strings.Builder
	for ip, name := range hosts {
		hostsContent.WriteString(fmt.Sprintf("%s %s\n", ip, name))
	}

	for ip := range hosts {
		fmt.Printf("📄 Checking /etc/hosts on %s...\n", ip)

		cmdCheckHosts := exec.Command("sshpass", "-p", sshPassword,
			"ssh", "-o", "StrictHostKeyChecking=no", "-tt",
			fmt.Sprintf("%s@%s", sshUser, ip),
			fmt.Sprintf("grep -Fxq '%s' /etc/hosts && echo 'exists'", strings.TrimSpace(hostsContent.String())))
		output, _ := cmdCheckHosts.CombinedOutput()

		if strings.Contains(string(output), "exists") {
			fmt.Println("📄 /etc/hosts entry already exists, skipping.")
		} else {
			cmd := exec.Command("sshpass", "-p", sshPassword,
				"ssh", "-o", "StrictHostKeyChecking=no", "-tt",
				fmt.Sprintf("%s@%s", sshUser, ip),
				fmt.Sprintf("echo '%s' | sudo -S tee -a /etc/hosts > /dev/null", hostsContent.String()))
			runCommand(cmd, fmt.Sprintf("Failed to configure /etc/hosts on %s", ip))
		}
	}
}

// Set up SSH between all machines
func setupSSH() {
	for ip := range hosts {
		fmt.Printf("🚀 Setting up passwordless SSH on %s...\n", ip)

		// Check if passwordless SSH is already set up
		cmdCheckSSH := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no",
			fmt.Sprintf("%s@%s", sshUser, ip), "echo success")
		output, _ := cmdCheckSSH.CombinedOutput()

		if strings.Contains(string(output), "success") {
			fmt.Printf("🔑 Passwordless SSH already configured on %s, skipping.\n", ip)
		} else {
			// If not configured, copy the SSH key to the target host
			cmdCopyKey := exec.Command("sshpass", "-p", sshPassword, "ssh-copy-id",
				"-o", "StrictHostKeyChecking=no", fmt.Sprintf("%s@%s", sshUser, ip))
			runCommand(cmdCopyKey, fmt.Sprintf("Failed to copy SSH key to %s", ip))
		}

		// Enable SSH between all nodes in the cluster
		for subIP := range hosts {
			if ip != subIP {
				fmt.Printf("🔁 Checking SSH between %s and %s...\n", ip, subIP)

				// Check if SSH is already set up between the nodes
				cmdCheckInterSSH := exec.Command("ssh", fmt.Sprintf("%s@%s", sshUser, ip),
					"ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no",
					fmt.Sprintf("%s@%s", sshUser, subIP), "echo success")
				interOutput, _ := cmdCheckInterSSH.CombinedOutput()

				if strings.Contains(string(interOutput), "success") {
					fmt.Printf("🔑 Passwordless SSH already configured between %s and %s, skipping.\n", ip, subIP)
				} else {
					// Copy SSH key to enable passwordless SSH between cluster nodes
					cmdInterCopyKey := exec.Command("ssh", fmt.Sprintf("%s@%s", sshUser, ip),
						"sshpass", "-p", sshPassword, "ssh-copy-id",
						"-o", "StrictHostKeyChecking=no", fmt.Sprintf("%s@%s", sshUser, subIP))
					runCommand(cmdInterCopyKey, fmt.Sprintf("Failed to enable SSH between %s and %s", ip, subIP))
				}
			}
		}
	}
}

// Helper function to run shell commands with error handling
func runCommand(cmd *exec.Cmd, errorMessage string) {
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("❌ %s: %s\n", errorMessage, string(output))
		os.Exit(1)
	}
	fmt.Println(string(output))
}

