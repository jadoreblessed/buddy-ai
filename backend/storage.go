package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func newApp(file string) (*App, error) {
	app := &App{file: file}

	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return app, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &app.request); err != nil {
		return nil, err
	}

	return app, nil
}

func (a *App) save(request *Request) error {
	dir := filepath.Dir(a.file)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp(dir, "request-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	return os.Rename(temp.Name(), a.file)
}
