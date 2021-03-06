// Package fileserver implements a gemini handler function to serve files with optional auto
// indexing for directory listings.
package fileserver

import (
	"errors"
	"fmt"
	"io/ioutil"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/n0x1m/gmifs/gemini"
)

var (
	ErrDirWithoutIndexFile = errors.New("path without index.gmi not allowed")
	ErrUnsupportedFileType = errors.New("disabled/unsupported file type")
)

func Serve(root string, autoindex bool) func(w gemini.ResponseWriter, r *gemini.Request) {
	return func(w gemini.ResponseWriter, r *gemini.Request) {
		fullpath, err := fullPath(root, r.URL.Path)
		if err != nil {
			if errors.Is(err, ErrDirWithoutIndexFile) && autoindex {
				body, mimeType, err := listDirectory(fullpath, r.URL.Path)
				if err != nil {
					w.WriteHeader(gemini.StatusNotFound, err.Error())
					return
				}

				w.WriteHeader(gemini.StatusSuccess, mimeType)
				w.Write(body)
				return
			}

			w.WriteHeader(gemini.StatusNotFound, err.Error())
			return
		}

		body, mimeType, err := readFile(fullpath)
		if err != nil {
			w.WriteHeader(gemini.StatusNotFound, err.Error())
			return
		}

		w.WriteHeader(gemini.StatusSuccess, mimeType)
		w.Write(body)
	}
}

func fullPath(root, requestPath string) (string, error) {
	fullpath := path.Join(root, requestPath)

	pathInfo, err := os.Stat(fullpath)
	if err != nil {
		return "", fmt.Errorf("path: %w", err)
	}

	if pathInfo.IsDir() {
		subDirIndex := path.Join(fullpath, gemini.IndexFile)
		if _, err := os.Stat(subDirIndex); os.IsNotExist(err) {
			return fullpath, ErrDirWithoutIndexFile
		}

		fullpath = subDirIndex
	}

	return fullpath, nil
}

func readFile(filepath string) ([]byte, string, error) {
	mimeType := getMimeType(filepath)
	if mimeType == "" {
		return nil, "", ErrUnsupportedFileType
	}

	file, err := os.Open(filepath)
	if err != nil {
		return nil, "", fmt.Errorf("file: %w", err)
	}
	defer file.Close()

	data, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, "", fmt.Errorf("read: %w", err)
	}
	return data, mimeType, nil
}

func getMimeType(fullpath string) string {
	if ext := path.Ext(fullpath); ext != ".gmi" {
		return mime.TypeByExtension(ext)
	}
	return gemini.MimeType
}

func listDirectory(fullpath, relpath string) ([]byte, string, error) {
	files, err := ioutil.ReadDir(fullpath)
	if err != nil {
		return nil, "", fmt.Errorf("list directory: %w", err)
	}

	var out []byte
	idx := strings.TrimRight(relpath, "/")
	parent := filepath.Dir(idx)
	if relpath != "/" {
		out = append(out, []byte(fmt.Sprintf("Index of %s/\n\n", idx))...)
		out = append(out, []byte(fmt.Sprintf("=> %s ..\n", parent))...)
	} else {
		out = append(out, []byte(fmt.Sprintf("Index of %s\n\n", relpath))...)
	}

	for _, f := range files {
		if relpath == "/" {
			out = append(out, []byte(fmt.Sprintf("=> %s\n", f.Name()))...)
		} else {
			out = append(out, []byte(fmt.Sprintf("=> %s/%s %s\n", relpath, f.Name(), f.Name()))...)
		}
	}

	return out, gemini.MimeType, nil
}
