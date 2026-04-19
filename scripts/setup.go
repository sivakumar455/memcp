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

	fmt.Println("\nBootstrapping default persona files in ~/.memcp/soul...")
	bootstrapPersona(cwd)

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

func bootstrapPersona(workspace string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	destDir := filepath.Join(homeDir, ".memcp", "soul")
	srcDir := filepath.Join(workspace, "soul")

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return
	}

	files := []string{"SOUL.md", "IDENTITY.md"}
	for _, f := range files {
		srcPath := filepath.Join(srcDir, f)
		destPath := filepath.Join(destDir, f)

		// Create only if it doesn't already exist
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			if data, err := os.ReadFile(srcPath); err == nil {
				os.WriteFile(destPath, data, 0644)
			}
		}
	}

	// Bootstrap skills directory if it exists in the workspace
	destSkillsDir := filepath.Join(homeDir, ".memcp", "skills")
	srcSkillsDir := filepath.Join(workspace, "skills")

	if _, err := os.Stat(srcSkillsDir); err == nil {
		os.MkdirAll(destSkillsDir, 0755)

		entries, _ := os.ReadDir(srcSkillsDir)
		for _, entry := range entries {
			if entry.IsDir() {
				srcSkillPath := filepath.Join(srcSkillsDir, entry.Name(), "SKILL.md")
				destSkillPath := filepath.Join(destSkillsDir, entry.Name(), "SKILL.md")

				if _, err := os.Stat(destSkillPath); os.IsNotExist(err) {
					if data, err := os.ReadFile(srcSkillPath); err == nil {
						os.MkdirAll(filepath.Join(destSkillsDir, entry.Name()), 0755)
						os.WriteFile(destSkillPath, data, 0644)
					}
				}
			}
		}
	}
}
