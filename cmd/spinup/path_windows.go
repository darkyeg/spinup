package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/darkyeg/spinup/internal/host"
)

const (
	environmentKey  = "Environment"
	pathValue       = "Path"
	broadcastWait   = 2000
	hwndBroadcast   = 0xffff
	wmSettingChange = 0x001A
	smtoAbortIfHung = 0x0002
)

// ensureOnPath adds dir to the user's PATH in the registry, so new terminals find what is in it.
func ensureOnPath(dir string) error {
	if host.OnPath(dir) {
		return nil
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, environmentKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	current, _, err := key.GetStringValue(pathValue)
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	updated, added := host.PathWith(current, dir)
	if !added {
		return nil
	}
	if err := key.SetExpandStringValue(pathValue, updated); err != nil {
		return err
	}
	broadcastEnvironmentChange()
	step("Added %s to your PATH (new terminals pick it up)", dir)
	return nil
}

func broadcastEnvironmentChange() {
	text, err := windows.UTF16PtrFromString(environmentKey)
	if err != nil {
		return
	}
	var result uintptr
	_, _, _ = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(
		hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(text)), smtoAbortIfHung, broadcastWait, uintptr(unsafe.Pointer(&result)))
}
