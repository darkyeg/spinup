package host

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// fsctlSetReparsePoint writes a reparse point; windows.FSCTL_SET_REPARSE_POINT is not exported.
const fsctlSetReparsePoint = 0x900A4

// LinkDir points link at target. On Windows that is a directory junction, which every user may make,
// unlike a symlink. It is written through the filesystem API: shelling out to `mklink` would flash a
// console window on every call, and spinup's service links skills every few minutes.
func LinkDir(link, target string) error {
	full, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	if err := writeMountPoint(link, full); err != nil {
		_ = os.Remove(link)
		return fmt.Errorf("junction %s -> %s: %w", link, full, err)
	}
	return nil
}

func writeMountPoint(link, target string) error {
	handle, err := openForReparse(link)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	buffer := mountPointBuffer(target)
	var returned uint32
	return windows.DeviceIoControl(handle, fsctlSetReparsePoint,
		&buffer[0], uint32(len(buffer)), nil, 0, &returned, nil)
}

func openForReparse(dir string) (windows.Handle, error) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(path, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
}

// mountPointBuffer lays out REPARSE_DATA_BUFFER for a mount point: the header, then the substitute
// name Windows resolves (`\??\C:\...`) and the print name it shows, each NUL-terminated.
func mountPointBuffer(target string) []byte {
	substitute := windows.StringToUTF16(`\??\` + target)
	print := windows.StringToUTF16(target)
	names := make([]byte, 0, (len(substitute)+len(print))*2)
	for _, run := range [][]uint16{substitute, print} {
		for _, unit := range run {
			names = binary.LittleEndian.AppendUint16(names, unit)
		}
	}
	const pathBufferOffset = 8 + 8 // reparse header, then the four name offsets and lengths
	substituteBytes := (len(substitute) - 1) * 2
	printBytes := (len(print) - 1) * 2

	buffer := make([]byte, pathBufferOffset+len(names))
	binary.LittleEndian.PutUint32(buffer[0:], windows.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(buffer[4:], uint16(8+len(names)))
	binary.LittleEndian.PutUint16(buffer[6:], 0)
	binary.LittleEndian.PutUint16(buffer[8:], 0)
	binary.LittleEndian.PutUint16(buffer[10:], uint16(substituteBytes))
	binary.LittleEndian.PutUint16(buffer[12:], uint16(len(substitute)*2))
	binary.LittleEndian.PutUint16(buffer[14:], uint16(printBytes))
	copy(buffer[pathBufferOffset:], names)
	return buffer
}
