package instance

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf16"
)

const (
	// errPrivilegeNotHeld is what CreateSymbolicLink says without Developer
	// Mode or an elevated token.
	errPrivilegeNotHeld syscall.Errno = 1314

	fsctlSetReparsePoint   = 0x000900A4
	ioReparseTagMountPoint = 0xA0000003

	// maxReparseData is the most a reparse buffer may carry.
	maxReparseData = 16 * 1024
)

// linkDir makes the kind of link older versions put in place of .minecraft:
// a symlink where Windows allows one, a junction where it does not.
func linkDir(target, link string) error {
	err := os.Symlink(target, link)
	if err == nil || !errors.Is(err, errPrivilegeNotHeld) {
		return err
	}
	return createJunction(target, link)
}

// createJunction makes link a directory junction to target: an empty
// directory carrying a mount point reparse buffer, which is all `mklink /J`
// does.
func createJunction(target, link string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}

	// The buffer holds the NT path the filesystem follows and the plain path
	// tools display, each NUL-terminated.
	sub := utf16.Encode([]rune(`\??\` + abs))
	display := utf16.Encode([]rune(abs))
	names := make([]uint16, 0, len(sub)+len(display)+2)
	names = append(names, sub...)
	names = append(names, 0)
	names = append(names, display...)
	names = append(names, 0)

	dataLen := 8 + 2*len(names)
	if dataLen > maxReparseData {
		return fmt.Errorf("%s is too long for a junction", abs)
	}
	buf := make([]byte, 8+dataLen)
	le := binary.LittleEndian
	le.PutUint32(buf[0:], ioReparseTagMountPoint)
	le.PutUint16(buf[4:], uint16(dataLen))
	le.PutUint16(buf[8:], 0)                       // substitute name offset
	le.PutUint16(buf[10:], uint16(2*len(sub)))     // substitute name length
	le.PutUint16(buf[12:], uint16(2*(len(sub)+1))) // print name offset
	le.PutUint16(buf[14:], uint16(2*len(display))) // print name length
	for i, c := range names {
		le.PutUint16(buf[16+2*i:], c)
	}

	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	name, err := syscall.UTF16PtrFromString(link)
	if err != nil {
		os.Remove(link)
		return err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		os.Remove(link)
		return fmt.Errorf("opening %s: %w", link, err)
	}
	var returned uint32
	err = syscall.DeviceIoControl(h, fsctlSetReparsePoint, &buf[0], uint32(len(buf)), nil, 0, &returned, nil)
	syscall.CloseHandle(h)
	if err != nil {
		os.Remove(link)
		return fmt.Errorf("making %s a junction: %w", link, err)
	}
	return nil
}
