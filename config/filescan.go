package config

// The file_scan_exclusions and file_scan_inclusions keys are the scan filters
// Zed's default.json defines (assets/settings/default.json): gitignore-style
// patterns relative to the workspace root. Exclusions hard-drop an entry from
// the file tree, the file finder and live grep whatever else matches it;
// inclusions clear the ignored flag of a .gitignore-matched entry so it scans
// like a normal file. An exclusion beats an inclusion.
//
// The defaults are Zed's, with the leading "**/" spelled the way an unanchored
// gitignore pattern already means: a name with no slash matches at any depth.
func DefaultFileScanExclusions() []string {
	return []string{
		".git", ".svn", ".hg", ".jj", ".sl", ".repo", "CVS",
		".DS_Store", "Thumbs.db", ".classpath", ".settings",
	}
}

// DefaultFileScanInclusions is Zed's default: .env files are often ignored yet
// are the files people open to edit.
func DefaultFileScanInclusions() []string {
	return []string{".env*"}
}
