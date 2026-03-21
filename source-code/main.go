package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "chker",
		Short: "Debian kernel manager",
		Long: `chker is a tool to manage kernels on Debian-based systems.
It can install Liquorix or XanMod kernels and revert to the Debian kernel.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	liquorixCmd = &cobra.Command{
		Use:   "liquorix",
		Short: "Install Liquorix kernel",
		Run: func(cmd *cobra.Command, args []string) {
			installLiquorix()
		},
	}

	xanmodCmd = &cobra.Command{
		Use:   "xanmod",
		Short: "Install XanMod kernel",
		Run: func(cmd *cobra.Command, args []string) {
			installXanmod()
		},
	}

	debCmd = &cobra.Command{
		Use:   "deb-kernel",
		Short: "Revert to Debian kernel",
		Run: func(cmd *cobra.Command, args []string) {
			installDebianKernel()
		},
	}
)

func main() {
	rootCmd.AddCommand(liquorixCmd, xanmodCmd, debCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// ------------------------------------------------------------
// Shared helpers
// ------------------------------------------------------------

func isRoot() bool {
	return os.Geteuid() == 0
}

func commandExists(cmd string) bool {
	path, err := exec.LookPath(cmd)
	return err == nil && path != ""
}

func runCommand(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runSudoCommand(args ...string) error {
	if isRoot() {
		return runCommand(args...)
	}
	cmd := exec.Command("sudo", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCommandOutput(args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("command %v failed: %v\nstderr: %s", args, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// ------------------------------------------------------------
// Parsing mappings (xanmod-cpu.hacker)
// ------------------------------------------------------------

func parseMappings(filename string) ([]string, error) {
	content, err := readFile(filename)
	if err != nil {
		return nil, err
	}
	var mappings []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inner := line[1 : len(line)-1]
			if strings.Contains(inner, ">") {
				mappings = append(mappings, inner)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return mappings, nil
}

func sortMappingsByLength(mappings []string) (patterns, targets []string) {
	type pair struct {
		pat string
		tar string
		len int
	}
	pairs := make([]pair, 0, len(mappings))
	for _, m := range mappings {
		parts := strings.SplitN(m, ">", 2)
		if len(parts) != 2 {
			continue
		}
		pat := strings.TrimSpace(parts[0])
		tar := strings.TrimSpace(parts[1])
		pairs = append(pairs, pair{pat, tar, len(pat)})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].len > pairs[j].len
	})
	patterns = make([]string, len(pairs))
	targets = make([]string, len(pairs))
	for i, p := range pairs {
		patterns[i] = p.pat
		targets[i] = p.tar
	}
	return
}

// ------------------------------------------------------------
// CPU info
// ------------------------------------------------------------

func getCPUModel() string {
	content, err := readFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Model name:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func getCPUVendor() string {
	content, err := readFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Vendor ID:") || strings.HasPrefix(line, "vendor_id:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func getCPUText() string {
	lscpu, err := runCommandOutput("lscpu")
	if err != nil {
		lscpu = ""
	}
	model := getCPUModel()
	vendor := getCPUVendor()
	return strings.ToLower(strings.TrimSpace(fmt.Sprintf("%s %s %s", model, vendor, lscpu)))
}

// ------------------------------------------------------------
// XanMod repo
// ------------------------------------------------------------

func addXanmodRepo() error {
	fmt.Println("[xanmod-installer] Adding XanMod repository...")
	if err := runSudoCommand("mkdir", "-p", "/etc/apt/keyrings"); err != nil {
		return err
	}
	if !commandExists("wget") {
		fmt.Println("[xanmod-installer] wget not found, installing...")
		if err := runSudoCommand("apt", "update"); err != nil {
			return err
		}
		if err := runSudoCommand("apt", "install", "-y", "wget"); err != nil {
			return err
		}
	}
	keyTemp := "/tmp/xanmod.key"
	if err := runCommand("wget", "-qO", keyTemp, "https://dl.xanmod.org/archive.key"); err != nil {
		return err
	}
	if err := runSudoCommand("gpg", "--dearmor", "-o", "/etc/apt/keyrings/xanmod-archive-keyring.gpg", keyTemp); err != nil {
		return err
	}
	release, err := runCommandOutput("lsb_release", "-sc")
	if err != nil {
		return err
	}
	listContent := fmt.Sprintf("deb [signed-by=/etc/apt/keyrings/xanmod-archive-keyring.gpg] http://deb.xanmod.org %s main\n", strings.TrimSpace(release))
	listFile := "/etc/apt/sources.list.d/xanmod-release.list"
	if err := os.WriteFile(listFile, []byte(listContent), 0644); err != nil {
		return err
	}
	return runSudoCommand("apt", "update")
}

// ------------------------------------------------------------
// NVIDIA detection and installation
// ------------------------------------------------------------

func checkAndInstallNvidia() error {
	fmt.Println("[check] Checking for NVIDIA GPU...")
	if !commandExists("lspci") {
		fmt.Println("[check] lspci not found, installing pciutils...")
		if err := runSudoCommand("apt", "update"); err != nil {
			return err
		}
		if err := runSudoCommand("apt", "install", "-y", "pciutils"); err != nil {
			return err
		}
	}
	out, err := runCommandOutput("lspci", "-nnk")
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(out), "nvidia") {
		fmt.Println("[check] No NVIDIA GPU detected.")
		return nil
	}
	fmt.Println("[check] NVIDIA detected, installing drivers...")
	if err := runSudoCommand("apt", "update"); err != nil {
		return err
	}
	packages := []string{
		"nvidia-driver",
		"nvidia-kernel-dkms",
		"nvidia-smi",
		"libnvidia-ml1",
		"nvidia-settings",
		"nvidia-cuda-mps",
	}
	for _, pkg := range packages {
		if err := runSudoCommand("apt", "install", "-y", pkg); err != nil {
			fmt.Printf("[check] Warning: failed to install %s: %v\n", pkg, err)
		}
	}
	return nil
}

// ------------------------------------------------------------
// Kernel removal helpers
// ------------------------------------------------------------

func getCurrentKernel() (string, error) {
	return runCommandOutput("uname", "-r")
}

func removeCurrentKernel() error {
	kernel, err := getCurrentKernel()
	if err != nil {
		return err
	}
	pkg := fmt.Sprintf("linux-image-%s", strings.TrimSpace(kernel))
	return runSudoCommand("apt", "remove", "--purge", "-y", pkg)
}

func removeDebianKernel() error {
	script := "/usr/share/HackerOS/Scripts/Bin/remove-debian-kernel.sh"
	if _, err := os.Stat(script); err == nil {
		return runSudoCommand(script)
	}
	fmt.Println("[kernel] Debian kernel removal script not found, falling back to remove current kernel.")
	return removeCurrentKernel()
}

// ------------------------------------------------------------
// Config file
// ------------------------------------------------------------

func createConfigFile(kernelType string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configDir := filepath.Join(home, ".config", "hackeros")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	configFile := filepath.Join(configDir, "kernel.hacker")
	data := fmt.Sprintf("[%s]\n", kernelType)
	return os.WriteFile(configFile, []byte(data), 0644)
}

// ------------------------------------------------------------
// XanMod installation
// ------------------------------------------------------------

func installXanmod() {
	logPrefix := "[xanmod-installer]"
	fmt.Printf("%s Start\n", logPrefix)
	tmpfile := "/tmp/xanmod-cpu.hacker"
	githubRaw := "https://raw.githubusercontent.com/HackerOS-Linux-System/Hacker-Lang/main/hacker-packages/xanmod-cpu.hacker"

	fmt.Printf("%s Downloading mapping file...\n", logPrefix)
	if !commandExists("curl") {
		fmt.Printf("%s curl not found, installing...\n", logPrefix)
		if err := runSudoCommand("apt", "update"); err != nil {
			fmt.Fprintf(os.Stderr, "%s Error: %v\n", logPrefix, err)
			os.Exit(1)
		}
		if err := runSudoCommand("apt", "install", "-y", "curl"); err != nil {
			fmt.Fprintf(os.Stderr, "%s Error: %v\n", logPrefix, err)
			os.Exit(1)
		}
	}
	if err := downloadFile(githubRaw, tmpfile); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to download mapping: %v\n", logPrefix, err)
		os.Exit(1)
	}
	defer os.Remove(tmpfile)

	// Parse mappings
	mappings, err := parseMappings(tmpfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s Error parsing mappings: %v\n", logPrefix, err)
		os.Exit(1)
	}
	if len(mappings) == 0 {
		fmt.Fprintf(os.Stderr, "%s No valid mappings found.\n", logPrefix)
		os.Exit(1)
	}
	patterns, targets := sortMappingsByLength(mappings)

	// CPU info
	cpuText := getCPUText()
	fmt.Printf("%s Detected CPU:\n%s\n", logPrefix, getCPUModel())

	// Match pattern
	selectedTarget := ""
	selectedPattern := ""
	for i, pat := range patterns {
		patLc := strings.ToLower(strings.TrimSpace(pat))
		if len(patLc) > 0 && strings.Contains(cpuText, patLc) {
			selectedTarget = targets[i]
			selectedPattern = pat
			break
		}
	}
	if selectedTarget == "" {
		// Fallback to "all x86-64"
		for i, pat := range patterns {
			if strings.Contains(strings.ToLower(pat), "all x86-64") {
				selectedTarget = targets[i]
				selectedPattern = pat
				break
			}
		}
	}
	if selectedTarget == "" {
		fmt.Printf("%s No match, using default x86-64\n", logPrefix)
		selectedTarget = "x86-64"
		selectedPattern = "(default x86-64)"
	}
	fmt.Printf("%s Matched: '%s' -> '%s'\n", logPrefix, selectedPattern, selectedTarget)

	// Variant mapping
	xanmodVariant := "x64v1"
	if strings.Contains(selectedTarget, "v3") {
		xanmodVariant = "x64v3"
	} else if strings.Contains(selectedTarget, "v2") {
		xanmodVariant = "x64v2"
	}
	fmt.Printf("%s Selected XanMod variant: %s\n", logPrefix, xanmodVariant)

	// Add repo and install
	if err := addXanmodRepo(); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to add XanMod repo: %v\n", logPrefix, err)
		os.Exit(1)
	}
	pkg := fmt.Sprintf("linux-xanmod-lts-%s", xanmodVariant)
	fmt.Printf("%s Installing: %s\n", logPrefix, pkg)
	if err := runSudoCommand("apt", "install", "-y", pkg); err != nil {
		fmt.Fprintf(os.Stderr, "%s Installation failed: %v\n", logPrefix, err)
		os.Exit(1)
	}
	fmt.Printf("%s Kernel installed.\n", logPrefix)

	// NVIDIA
	if err := checkAndInstallNvidia(); err != nil {
		fmt.Fprintf(os.Stderr, "%s NVIDIA installation warning: %v\n", logPrefix, err)
	}

	// Remove Debian kernel
	if err := removeDebianKernel(); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to remove Debian kernel: %v\n", logPrefix, err)
	}

	// Config file
	if err := createConfigFile("xanmod"); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to create config file: %v\n", logPrefix, err)
	}

	// Update GRUB
	fmt.Printf("%s Updating GRUB...\n", logPrefix)
	if err := runSudoCommand("update-grub"); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to update GRUB: %v\n", logPrefix, err)
		os.Exit(1)
	}
	fmt.Printf("%s Done. Reboot to use the new kernel.\n", logPrefix)
}

// ------------------------------------------------------------
// Liquorix installation
// ------------------------------------------------------------

func installLiquorix() {
	logPrefix := "[liquorix-installer]"
	fmt.Printf("%s Start\n", logPrefix)
	tmpfile := "/tmp/xanmod-cpu.hacker"
	githubRaw := "https://raw.githubusercontent.com/HackerOS-Linux-System/Hacker-Lang/main/hacker-packages/xanmod-cpu.hacker"

	fmt.Printf("%s Downloading mapping file...\n", logPrefix)
	if !commandExists("curl") {
		fmt.Printf("%s curl not found, installing...\n", logPrefix)
		if err := runSudoCommand("apt", "update"); err != nil {
			fmt.Fprintf(os.Stderr, "%s Error: %v\n", logPrefix, err)
			os.Exit(1)
		}
		if err := runSudoCommand("apt", "install", "-y", "curl"); err != nil {
			fmt.Fprintf(os.Stderr, "%s Error: %v\n", logPrefix, err)
			os.Exit(1)
		}
	}
	if err := downloadFile(githubRaw, tmpfile); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to download mapping: %v\n", logPrefix, err)
		os.Exit(1)
	}
	defer os.Remove(tmpfile)

	// Parse mappings
	mappings, err := parseMappings(tmpfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s Error parsing mappings: %v\n", logPrefix, err)
		os.Exit(1)
	}
	if len(mappings) == 0 {
		fmt.Fprintf(os.Stderr, "%s No valid mappings found.\n", logPrefix)
		os.Exit(1)
	}
	patterns, targets := sortMappingsByLength(mappings)

	// CPU info
	cpuText := getCPUText()
	fmt.Printf("%s Detected CPU:\n%s\n", logPrefix, getCPUModel())

	// Match pattern
	selectedTarget := ""
	selectedPattern := ""
	for i, pat := range patterns {
		patLc := strings.ToLower(strings.TrimSpace(pat))
		if len(patLc) > 0 && strings.Contains(cpuText, patLc) {
			selectedTarget = targets[i]
			selectedPattern = pat
			break
		}
	}
	if selectedTarget == "" {
		for i, pat := range patterns {
			if strings.Contains(strings.ToLower(pat), "all x86-64") {
				selectedTarget = targets[i]
				selectedPattern = pat
				break
			}
		}
	}
	if selectedTarget == "" {
		fmt.Printf("%s No match, using default x86-64\n", logPrefix)
		selectedTarget = "x86-64"
		selectedPattern = "(default x86-64)"
	}
	fmt.Printf("%s Matched: '%s' -> '%s'\n", logPrefix, selectedPattern, selectedTarget)

	// Variant mapping
	cpuVariant := "x64v1"
	if strings.Contains(selectedTarget, "v3") {
		cpuVariant = "x64v3"
	} else if strings.Contains(selectedTarget, "v2") {
		cpuVariant = "x64v2"
	}
	fmt.Printf("%s CPU variant: %s\n", logPrefix, cpuVariant)

	// Run Liquorix install script
	fmt.Printf("%s Installing Liquorix kernel...\n", logPrefix)
	scriptURL := "https://liquorix.net/install-liquorix.sh"
	scriptPath := "/tmp/install-liquorix.sh"
	if err := downloadFile(scriptURL, scriptPath); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to download Liquorix installer: %v\n", logPrefix, err)
		os.Exit(1)
	}
	defer os.Remove(scriptPath)
	if err := runSudoCommand("bash", scriptPath); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to run Liquorix installer: %v\n", logPrefix, err)
		os.Exit(1)
	}
	fmt.Printf("%s Liquorix kernel installed.\n", logPrefix)

	// NVIDIA
	if err := checkAndInstallNvidia(); err != nil {
		fmt.Fprintf(os.Stderr, "%s NVIDIA installation warning: %v\n", logPrefix, err)
	}

	// Remove Debian kernel
	if err := removeDebianKernel(); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to remove Debian kernel: %v\n", logPrefix, err)
	}

	// Config file
	if err := createConfigFile("liquorix"); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to create config file: %v\n", logPrefix, err)
	}

	fmt.Printf("%s Done. Reboot to use the new kernel.\n", logPrefix)
}

// ------------------------------------------------------------
// Debian kernel installation
// ------------------------------------------------------------

func installDebianKernel() {
	logPrefix := "[deb-kernel-installer]"
	cur, err := getCurrentKernel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to get current kernel: %v\n", logPrefix, err)
		os.Exit(1)
	}
	if strings.Contains(cur, "debian") || (!strings.Contains(cur, "liquorix") && !strings.Contains(cur, "xanmod")) {
		fmt.Printf("%s Debian kernel already in use or no alternative kernel found. Nothing to do.\n", logPrefix)
		return
	}
	fmt.Printf("%s Removing current kernel and installing Debian kernel.\n", logPrefix)
	if err := removeCurrentKernel(); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to remove current kernel: %v\n", logPrefix, err)
	}
	if err := runSudoCommand("apt", "update"); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to update package list: %v\n", logPrefix, err)
	}
	if err := runSudoCommand("apt", "install", "-y", "linux-image-amd64"); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to install Debian kernel: %v\n", logPrefix, err)
	}
	if err := runSudoCommand("update-grub"); err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to update GRUB: %v\n", logPrefix, err)
	}
	home, _ := os.UserHomeDir()
	configFile := filepath.Join(home, ".config", "hackeros", "kernel.hacker")
	os.Remove(configFile)
	fmt.Printf("%s Done. Reboot to apply.\n", logPrefix)
}
