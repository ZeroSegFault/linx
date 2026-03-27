// Copyright (C) 2026 Ashley Stonham
// SPDX-License-Identifier: AGPL-3.0-only
// For commercial licensing, see COMMERCIAL.md

package main

import (
	"os"

	"github.com/ZeroSegFault/linx/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
