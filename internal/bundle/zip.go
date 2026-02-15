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
// Returns the path to the created zip file.
func CreateZip(outDir string, files []string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	zipName := fmt.Sprintf("nvcheckup-bundle-%s.zip", timestamp)
	zipPath := filepath.Join(outDir, zipName)

	zipFile, err := os.Create(zipPath)
	if err != nil {
		return "", fmt.Errorf("cannot create zip file: %w", err)
	}
	defer zipFile.Close()

	w := zip.NewWriter(zipFile)
	defer w.Close()

	for _, filePath := range files {
		if err := addFileToZip(w, filePath); err != nil {
			// Don't fail the whole bundle for one file
			fmt.Fprintf(os.Stderr, "  Warning: could not add %s to zip: %v\n", filePath, err)
			continue
		}
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
