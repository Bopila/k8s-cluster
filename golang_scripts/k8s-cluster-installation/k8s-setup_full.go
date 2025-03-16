package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"path/filepath"
)

var nodes = []string{
	"control-plane",
	"worker-node1",
	"worker-node2",
	"worker-node3",
}

const kubernetesVersion = "1.30.0" // Specify your desired version here

func runCommand(node, cmd string) error {
	fullCmd := fmt.Sprintf("ssh %s \"%s\"", node, cmd)
	log.Printf("Running on %s: %s", node, cmd)
	out, err := exec.Command("bash", "-c", fullCmd).CombinedOutput()
	if err != nil {
		log.Printf("Error on %s: %s", node, string(out))
		return err
	}
	log.Printf("Success on %s: %s", node, string(out))
	return nil
}

func configureFirewall(node string) error {
	cmds := []string{
		"sudo ufw allow 6443/tcp",
		"sudo ufw allow 2379:2380/tcp",
		"sudo ufw allow 10250/tcp",
		"sudo ufw allow 10251/tcp",
		"sudo ufw allow 10252/tcp",
		"sudo ufw allow 30000:32767/tcp",
		"sudo ufw enable",
	}

	for _, cmd := range cmds {
		if err := runCommand(node, cmd); err != nil {
			return err
		}
	}
	return nil
}

func copyCertificates(node string) error {
	certFiles := []string{
		"/home/user/KAINOS-INSPECTION_G2.crt",
		"/home/user/KAINOS-ROOT-CA_G2.crt",
		"/home/user/KAINOS-ZSCALER_G2.crt",
	}

	for _, certPath := range certFiles {
		certName := filepath.Base(certPath)
		destPath := fmt.Sprintf("/usr/local/share/ca-certificates/%s", certName)

		checkCmd := fmt.Sprintf("[ -f %s ] && echo 'exists'", destPath)
		out, _ := exec.Command("bash", "-c", fmt.Sprintf("ssh %s \"%s\"", node, checkCmd)).Output()
		if strings.TrimSpace(string(out)) == "exists" {
			log.Printf("Certificate %s already exists on %s, skipping.", certName, node)
			continue
		}

		scpCmd := fmt.Sprintf("scp %s %s:/tmp/%s", certPath, node, certName)
		log.Printf("Executing SCP: %s", scpCmd)
		if err := exec.Command("bash", "-c", scpCmd).Run(); err != nil {
			log.Printf("SCP failed for %s: %v", certPath, err)
			return err
		}

		installCmd := fmt.Sprintf("sudo mv /tmp/%s %s && sudo update-ca-certificates", certName, destPath)
		if err := runCommand(node, installCmd); err != nil {
			return err
		}
	}
	return nil
}

func enableIPv4Forwarding(node string) error {
	checkCmd := "sysctl net.ipv4.ip_forward"
	out, _ := exec.Command("bash", "-c", fmt.Sprintf("ssh %s \"%s\"", node, checkCmd)).Output()
	if strings.Contains(string(out), "= 1") {
		log.Printf("IPv4 forwarding already enabled on %s, skipping.", node)
		return nil
	}

	cmds := []string{
		"echo 'net.ipv4.ip_forward=1' | sudo tee /etc/sysctl.d/99-kubernetes-cri.conf",
		"sudo sysctl --system",
	}

	for _, cmd := range cmds {
		if err := runCommand(node, cmd); err != nil {
			return err
		}
	}
	return nil
}

func disableSwap(node string) error {
	// Disable swap and ensure it remains off after reboot
	cmd := "sudo swapoff -a && sudo sed -i '/swap/d' /etc/fstab"
	if err := runCommand(node, cmd); err != nil {
		return fmt.Errorf("failed to disable swap on %s: %v", node, err)
	}
	log.Printf("Swap disabled on %s", node)
	return nil
}

func setupKubernetesRepository(node string) error {
	cmds := []string{
		"curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.30/deb/Release.key | sudo gpg --batch --yes --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg",
		"echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.30/deb/ /' | sudo tee /etc/apt/sources.list.d/kubernetes.list",
		"sudo apt update",
	}
	for _, cmd := range cmds {
		if err := runCommand(node, cmd); err != nil {
			return err
		}
	}
	return nil
}

func installKubernetesPackages(node string) error {
	cmds := []string{
		"sudo apt install -y kubelet kubeadm kubectl",
		"sudo apt-mark hold kubelet kubeadm kubectl", // Mark as held to prevent accidental upgrades
	}
	for _, cmd := range cmds {
		if err := runCommand(node, cmd); err != nil {
			return err
		}
	}
	return nil
}

func setupContainerd(node string) error {
	cmds := []string{
		"sudo mkdir -p /etc/containerd",
		"containerd config default | sudo tee /etc/containerd/config.toml > /dev/null",
		"sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml",
		"sudo systemctl restart containerd",
	}
	for _, cmd := range cmds {
		if err := runCommand(node, cmd); err != nil {
			return err
		}
	}
	log.Printf("Containerd setup complete on %s", node)
	return nil
}

func initControlPlane() (string, error) {
	checkCmd := "test -f /etc/kubernetes/manifests/kube-apiserver.yaml && echo 'exists'"
	out, _ := exec.Command("bash", "-c", fmt.Sprintf("ssh control-plane \"%s\"", checkCmd)).Output()
	if strings.TrimSpace(string(out)) == "exists" {
		log.Println("Control plane already initialized, skipping kubeadm init.")
		return "", nil
	}

	cmd := "sudo kubeadm init --pod-network-cidr=192.168.0.0/16"
	err := runCommand("control-plane", cmd)
	if err != nil {
		return "", err
	}

	copyAdminConfCmd := "mkdir -p $HOME/.kube && sudo cp -f /etc/kubernetes/admin.conf $HOME/.kube/config && sudo chown $(id -u):$(id -g) $HOME/.kube/config"
	if err := runCommand("control-plane", copyAdminConfCmd); err != nil {
		return "", fmt.Errorf("failed to set up kubeconfig on control-plane: %v", err)
	}

	joinCmd := "kubeadm token create --print-join-command"
	out, err = exec.Command("bash", "-c", fmt.Sprintf("ssh control-plane \"%s\"", joinCmd)).Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}

func joinWorkerNodes(joinCmd string) error {
	if joinCmd == "" {
		log.Println("Worker nodes already joined, skipping.")
		return nil
	}

	for _, node := range nodes[1:] {
		joinCmdFull := fmt.Sprintf("sudo %s", joinCmd)
		if err := runCommand(node, joinCmdFull); err != nil {
			return err
		}
	}
	return nil
}

func setupRemoteKubectl() error {
	scpCmd := "scp control-plane:$HOME/.kube/config $HOME/.kube/config && sudo chown $(id -u):$(id -g) $HOME/.kube/config"
	log.Printf("Executing SCP: %s", scpCmd)
	if err := exec.Command("bash", "-c", scpCmd).Run(); err != nil {
		log.Printf("Failed to copy admin.conf: %v", err)
		return err
	}
	return nil
}

func installNetworkPlugin() error {
	cmds := []string{
		"kubectl create -f https://raw.githubusercontent.com/projectcalico/calico/v3.25.0/manifests/tigera-operator.yaml",
		"kubectl apply -f https://docs.projectcalico.org/manifests/calico.yaml",
	}
	for _, cmd := range cmds {
		if err := runCommand("control-plane", cmd); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	for _, node := range nodes {
		if err := configureFirewall(node); err != nil {
			log.Fatalf("Failed to configure firewall on %s: %v", node, err)
		}
		if err := copyCertificates(node); err != nil {
			log.Fatalf("Failed to copy certificates to %s: %v", node, err)
		}
		if err := enableIPv4Forwarding(node); err != nil {
			log.Fatalf("Failed to enable IPv4 forwarding on %s: %v", node, err)
		}
		if err := disableSwap(node); err != nil {
			log.Fatalf("Failed to disable swap on %s: %v", node, err)
		}
		if err := setupKubernetesRepository(node); err != nil {
			log.Fatalf("Failed to set up Kubernetes repository on %s: %v", node, err)
		}
		if err := installKubernetesPackages(node); err != nil {
			log.Fatalf("Failed to install Kubernetes packages on %s: %v", node, err)
		}
		if err := setupContainerd(node); err != nil {
			log.Fatalf("Failed to set up containerd on %s: %v", node, err)
		}
	}

	joinCmd, err := initControlPlane()
	if err != nil {
		log.Fatalf("Failed to initialize control-plane: %v", err)
	}

	if err := joinWorkerNodes(joinCmd); err != nil {
		log.Fatalf("Failed to join worker nodes: %v", err)
	}

	if err := setupRemoteKubectl(); err != nil {
		log.Fatalf("Failed to set up remote kubectl access: %v", err)
	}

	if err := installNetworkPlugin(); err != nil {
		log.Fatalf("Failed to install network plugin: %v", err)
	}
}
