package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ZeroSegFault/linx/backup"
	"github.com/ZeroSegFault/linx/memory"
)

func runRollback(last int) error {
	idx, err := backup.LoadIndex()
	if err != nil {
		return fmt.Errorf("loading backup index: %w", err)
	}

	if len(idx.Entries) == 0 {
		fmt.Println("No backups found.")
		return nil
	}

	n := last
	if n <= 0 {
		n = 20
	}

	entries := idx.LastN(n)
	fmt.Printf("Last %d backups:\n\n", len(entries))

	for i, e := range entries {
		ts := e.Timestamp
		if len(ts) > 19 {
			ts = ts[:19]
		}
		desc := e.Description
		if desc == "" {
			desc = "auto-backup"
		}
		fmt.Printf("  %2d. [%s] %s (%s)\n", i+1, ts, e.OriginalPath, desc)
	}

	fmt.Println()
	fmt.Print("Enter number(s) to restore (comma-separated), or 'q' to quit: ")

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)

	if answer == "q" || answer == "" {
		fmt.Println("Cancelled.")
		return nil
	}

	parts := strings.Split(answer, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		num, err := strconv.Atoi(part)
		if err != nil || num < 1 || num > len(entries) {
			fmt.Printf("Invalid selection: %s\n", part)
			continue
		}

		entry := entries[num-1]
		fmt.Printf("\nRestore %s → %s?\n", entry.BackupPath, entry.OriginalPath)
		fmt.Print("Confirm [y/N]: ")

		confirm, _ := reader.ReadString('\n')
		confirm = strings.TrimSpace(strings.ToLower(confirm))

		if confirm != "y" && confirm != "yes" {
			fmt.Println("Skipped.")
			continue
		}

		if err := backup.Restore(entry); err != nil {
			fmt.Printf("Error restoring: %v\n", err)
			continue
		}

		fmt.Printf("✓ Restored %s\n", entry.OriginalPath)
		_ = memory.AppendHistory(time.Now(), fmt.Sprintf("Rolled back %s from backup %s", entry.OriginalPath, entry.BackupPath))
	}

	return nil
}
