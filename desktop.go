package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// DesktopService exposes native dialogs to the frontend.
type DesktopService struct {
	app    *application.App
	window application.Window
}

// ChooseFolder opens a folder picker; returns "" when cancelled.
func (d *DesktopService) ChooseFolder(title string) (string, error) {
	dlg := d.app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true)
	if d.window != nil {
		dlg = dlg.AttachToWindow(d.window)
	}
	return dlg.PromptForSingleSelection()
}

// ChooseFile opens a file picker filtered by pattern (e.g. "*.xlsx").
func (d *DesktopService) ChooseFile(title, filterName, pattern string) (string, error) {
	dlg := d.app.Dialog.OpenFile().SetTitle(title).CanChooseFiles(true)
	if pattern != "" {
		dlg = dlg.AddFilter(filterName, pattern)
	}
	if d.window != nil {
		dlg = dlg.AttachToWindow(d.window)
	}
	return dlg.PromptForSingleSelection()
}

// OpenPath opens a file or folder with the default application.
func (d *DesktopService) OpenPath(path string) error {
	return d.app.Browser.OpenFile(path)
}
