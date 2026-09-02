// Package bundle creates zip archives of reports and logs.
package bundle

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// CreateZip creates a zip bundle containing the specified files.
// Returns the path to the created zip file. Files that cannot be read are
// skipped with a warning; a failure to finalize the archive itself is fatal
// because it would leave a truncated, unreadable zip behind.
func CreateZip(outDir string, files []string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	zipName := fmt.Sprintf("nvcheckup-bundle-%s.zip", timestamp)
	zipPath := filepath.Join(outDir, zipName)

	zipFile, err := os.Create(zipPath)
	if err != nil {
		return "", fmt.Errorf("cannot create zip file: %w", err)
	}

	w := zip.NewWriter(zipFile)
	for _, filePath := range files {
		if err := addFileToZip(w, filePath); err != nil {
			// Don't fail the whole bundle for one file
			fmt.Fprintf(os.Stderr, "  Warning: could not add %s to zip: %v\n", filePath, err)
			continue
		}
	}

	// The central directory is written on Close; an error here means the
	// archive is not valid, so report it rather than returning a bad path.
	if err := w.Close(); err != nil {
		zipFile.Close()
		return "", fmt.Errorf("cannot finalize zip archive: %w", err)
	}
	if err := zipFile.Close(); err != nil {
		return "", fmt.Errorf("cannot close zip file: %w", err)
	}

	return zipPath, nil
}

func addFileToZip(w *zip.Writer, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}

	// Use just the filename, not the full path
	header.Name = filepath.Base(filePath)
	header.Method = zip.Deflate

	writer, err := w.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = io.Copy(writer, file)
	return err
}
