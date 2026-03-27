package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZeroSegFault/linx/memory"
)

func runMemory(action string) error {
	switch action {
	case "list":
		content, err := memory.LoadRaw()
		if err != nil {
			return fmt.Errorf("loading memory: %w", err)
		}
		if content == "" {
			fmt.Println("Memory is empty. Linx hasn't learned anything about this system yet.")
			fmt.Printf("Memory file: %s\n", memory.MemoryPath())
			return nil
		}
		fmt.Print(content)
		fmt.Printf("\n---\nMemory file: %s\n", memory.MemoryPath())
		return nil

	case "edit":
		path := memory.MemoryPath()
		if _, err := os.Stat(path); os.IsNotExist(err) {
			mem := &memory.Memory{}
			if err := mem.Save(); err != nil {
				return fmt.Errorf("creating memory file: %w", err)
			}
			fmt.Printf("Created %s\n", path)
		}

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			editor = "vi"
		}

		c := exec.Command(editor, path)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()

	case "clear":
		content, err := memory.LoadRaw()
		if err != nil {
			return fmt.Errorf("loading memory: %w", err)
		}
		if content == "" {
			fmt.Println("Memory is already empty.")
			return nil
		}

		fmt.Printf("This will wipe all persistent memory at %s\n", memory.MemoryPath())
		fmt.Print("Are you sure? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))

		if answer != "y" && answer != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}

		if err := os.Remove(memory.MemoryPath()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing memory file: %w", err)
		}

		fmt.Println("Memory cleared.")
		return nil

	default:
		return fmt.Errorf("unknown memory action %q", action)
	}
}

func runHistory() error {
	dir := memory.HistoryDir()

	// Find history files, sorted newest first
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No session history yet.")
			return nil
		}
		return fmt.Errorf("reading history dir: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("No session history yet.")
		return nil
	}

	// Show last N days (use --last flag, default 3)
	days := flagLast
	if days <= 0 {
		days = 3
	}

	// Sort entries newest first
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			files = append(files, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	if len(files) > days {
		files = files[:days]
	}

	for _, f := range files {
		path := filepath.Join(dir, f)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fmt.Println(string(data))
	}

	fmt.Printf("---\nHistory dir: %s\n", dir)
	return nil
}
