package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type MCPServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

type ClaudeConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

func main() {
	fmt.Println("=====================================")
	fmt.Println("  memcp Setup & Configuration Tool   ")
	fmt.Println("=====================================")

	// 1. Build the binary
	fmt.Println("\nBuilding memcp...")
	cmd := exec.Command("make", "build")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error building memcp: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	binPath := filepath.Join(cwd, "bin", "memcp")
	fmt.Printf("Build successful! Binary located at: %s\n", binPath)

	fmt.Println("\nBootstrapping ~/.memcp/ data directory...")
	bootstrapAll(cwd)

	reader := bufio.NewReader(os.Stdin)

	// List common config locations
	homeDir, _ := os.UserHomeDir()
	configPaths := map[string]string{
		"Antigravity":    filepath.Join(homeDir, ".gemini", "antigravity", "mcp_config.json"),
		"Claude Desktop": filepath.Join(homeDir, "Library", "Application Support", "Claude", "claude_desktop_config.json"),
	}

	for name, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("\nFound %s configuration at:\n%s\n", name, path)
			fmt.Print("Would you like to manage MCP servers for this config? [y/N]: ")
			ans, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(ans)) == "y" {
				manageConfig(path, binPath, reader)
			}
		}
	}

	fmt.Println("\nSetup complete!")
}

func manageConfig(configPath, memcpBin string, reader *bufio.Reader) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Printf("Error reading config: %v\n", err)
		return
	}

	var cfg map[string]interface{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			fmt.Printf("Error parsing JSON config: %v\n", err)
			return
		}
	} else {
		cfg = make(map[string]interface{})
	}

	rawServers, ok := cfg["mcpServers"].(map[string]interface{})
	if !ok {
		rawServers = make(map[string]interface{})
		cfg["mcpServers"] = rawServers
	}

	// Install memcp
	fmt.Print("\n1. Install memcp server? (persistent memory & persona tools) [y/N]: ")
	ans, _ := reader.ReadString('\n')
	if strings.ToLower(strings.TrimSpace(ans)) == "y" {
		args := make([]string, 0)
		fmt.Print("   -> Enable daemon add-on? (background scheduler & HTTP gateway) [y/N]: ")
		ans2, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(ans2)) == "y" {
			args = append(args, "--daemon", "--http")
			fmt.Println("  -> Added 'memcp' server with daemon enabled.")
		} else {
			fmt.Println("  -> Added 'memcp' server.")
		}

		rawServers["memcp"] = map[string]interface{}{
			"command": memcpBin,
			"args":    args,
		}

		// Clean up the legacy separate daemon server entry if it was previously setup
		delete(rawServers, "memcp-daemon")
	}

	// Shim existing (Per MCP)
	existingCount := 0
	for srvName := range rawServers {
		if srvName != "memcp" && srvName != "memcp-daemon" && !strings.HasPrefix(srvName, "shim-") {
			existingCount++
		}
	}

	if existingCount > 0 {
		fmt.Printf("\nFound %d existing server(s). Checking for Shim Mode conversion:\n", existingCount)
		for srvName, srvRaw := range rawServers {
			if srvName == "memcp" || srvName == "memcp-daemon" || strings.HasPrefix(srvName, "shim-") {
				continue
			}

			srv, ok := srvRaw.(map[string]interface{})
			if !ok {
				continue
			}

			cmdRaw, _ := srv["command"].(string)
			if cmdRaw == memcpBin {
				continue // already wrapped implicitly
			}

			fmt.Printf("  -> Wrap '%s' (command: %s) in shim mode? [y/N]: ", srvName, cmdRaw)
			ans, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(ans)) == "y" {
				newArgs := []string{"--shim", "--name", srvName, "--", cmdRaw}

				origArgsRaw, _ := srv["args"].([]interface{})
				for _, oa := range origArgsRaw {
					if strArg, ok := oa.(string); ok {
						newArgs = append(newArgs, strArg)
					}
				}

				srv["command"] = memcpBin
				srv["args"] = newArgs

				fmt.Printf("      [OK] Shim applied to '%s'\n", srvName)
			} else {
				fmt.Printf("      [Skip] '%s' left unchanged.\n", srvName)
			}
		}
	}

	// Write back
	outData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Printf("Error encoding updated config: %v\n", err)
		return
	}

	if err := os.WriteFile(configPath, outData, 0644); err != nil {
		fmt.Printf("Error writing updated config: %v\n", err)
		return
	}

	fmt.Printf("\nSuccessfully updated %s!\n", configPath)
}

// resolveSiteDir returns site/<subdir> if it exists, otherwise <subdir>.
// This lets site/ content (company-specific) take priority over upstream defaults.
func resolveSiteDir(workspace, subdir string) string {
	siteDir := filepath.Join(workspace, "site", subdir)
	if info, err := os.Stat(siteDir); err == nil && info.IsDir() {
		return siteDir
	}
	return filepath.Join(workspace, subdir)
}

// bootstrapAll copies configs, soul, and skills into ~/.memcp/ on first run.
// Prefers site/ content over upstream defaults when available.
//
// With the two-file evolution model, authored files (IDENTITY.md, MEMORY.md,
// SKILL.md, configs) can be safely overwritten from site/ because all
// system-generated content lives in separate .evolved.md files that are
// never touched by bootstrap.
func bootstrapAll(workspace string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	memcpDir := filepath.Join(homeDir, ".memcp")

	bootstrapConfigs(workspace, memcpDir)
	bootstrapSoul(workspace, memcpDir)
	bootstrapSkills(workspace, memcpDir)
}

func bootstrapConfigs(workspace, memcpDir string) {
	srcDir := resolveSiteDir(workspace, "configs")
	destDir := filepath.Join(memcpDir, "configs")
	os.MkdirAll(destDir, 0755)

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		copyOrUpdate(
			filepath.Join(srcDir, entry.Name()),
			filepath.Join(destDir, entry.Name()),
		)
	}
	fmt.Printf("  configs: %s -> %s\n", srcDir, destDir)
}

func bootstrapSoul(workspace, memcpDir string) {
	srcDir := resolveSiteDir(workspace, "soul")
	destDir := filepath.Join(memcpDir, "soul")
	os.MkdirAll(destDir, 0755)

	// Only copy authored persona files. .evolved.md files are system-generated
	// and must never be overwritten or seeded by bootstrap.
	for _, f := range []string{"SOUL.md", "IDENTITY.md", "MEMORY.md"} {
		copyOrUpdate(
			filepath.Join(srcDir, f),
			filepath.Join(destDir, f),
		)
	}
	fmt.Printf("  soul:    %s -> %s\n", srcDir, destDir)
}

func bootstrapSkills(workspace, memcpDir string) {
	srcDir := resolveSiteDir(workspace, "skills")
	destDir := filepath.Join(memcpDir, "skills")

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return
	}

	os.MkdirAll(destDir, 0755)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Only copy authored SKILL.md; SKILL.evolved.md is system-generated.
		srcPath := filepath.Join(srcDir, entry.Name(), "SKILL.md")
		destPath := filepath.Join(destDir, entry.Name(), "SKILL.md")
		os.MkdirAll(filepath.Join(destDir, entry.Name()), 0755)
		copyOrUpdate(srcPath, destPath)
	}
	fmt.Printf("  skills:  %s -> %s\n", srcDir, destDir)
}

// copyOrUpdate copies src to dest, overwriting if the source has changed.
// With the two-file model, authored files can be safely refreshed because
// all learned/evolved content lives in separate .evolved.md files.
func copyOrUpdate(src, dest string) {
	srcData, err := os.ReadFile(src)
	if err != nil {
		return
	}
	destData, _ := os.ReadFile(dest)
	if string(srcData) == string(destData) {
		return // already up-to-date
	}
	os.WriteFile(dest, srcData, 0644)
}

// copyIfMissing copies src to dest only if dest does not exist.
func copyIfMissing(src, dest string) {
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		return
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	os.WriteFile(dest, data, 0644)
}
