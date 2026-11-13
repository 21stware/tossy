package trash

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// Recycle Bin via SHFileOperationW(FO_DELETE, FOF_ALLOWUNDO). Afterwards the matching
// $Recycle.Bin\<SID>\$I… record is located so the item can be restored or purged from FanFree.

type osMover struct{}

var (
	shell32         = syscall.NewLazyDLL("shell32.dll")
	shFileOperation = shell32.NewProc("SHFileOperationW")
)

type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete          = 3
	fofSilent         = 0x0004
	fofNoConfirmation = 0x0010
	fofAllowUndo      = 0x0040
	fofNoErrorUI      = 0x0400
)

func userSID() (string, error) {
	tok, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return "", err
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String()
}

func (osMover) trash(p string) (string, string, error) {
	from := utf16.Encode([]rune(p + "\x00\x00"))
	op := shFileOpStruct{wFunc: foDelete, pFrom: &from[0], fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI}
	t0 := time.Now().Add(-2 * time.Second)
	r, _, _ := shFileOperation.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return "", "", syscall.Errno(r)
	}
	if op.fAnyOperationsAborted != 0 {
		return "", "", errors.New("recycle aborted")
	}
	if _, err := os.Lstat(p); err == nil {
		return "", "", errors.New("item still present after recycling (too large for the Recycle Bin?)")
	}
	sid, err := userSID()
	if err != nil {
		return "", "", nil // recycled, but FanFree cannot locate it for restore
	}
	bin := filepath.Join(filepath.VolumeName(p)+`\`, "$Recycle.Bin", sid)
	ents, _ := os.ReadDir(bin)
	for _, e := range ents {
		n := e.Name()
		if !strings.HasPrefix(n, "$I") {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.ModTime().Before(t0) {
			continue
		}
		info := filepath.Join(bin, n)
		if orig, ok := readInfo(info); ok && strings.EqualFold(orig, p) {
			return filepath.Join(bin, "$R"+n[2:]), info, nil
		}
	}
	return "", "", nil
}

// $I file: int64 version, int64 size, FILETIME; v1: WCHAR[260]; v2: int32 length + WCHAR[length].
func readInfo(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) < 24 {
		return "", false
	}
	var u []uint16
	switch binary.LittleEndian.Uint64(b) {
	case 1:
		for i := 24; i+1 < len(b) && i < 24+520; i += 2 {
			u = append(u, binary.LittleEndian.Uint16(b[i:]))
		}
	case 2:
		if len(b) < 28 {
			return "", false
		}
		n := int(binary.LittleEndian.Uint32(b[24:]))
		for i := 28; i+1 < len(b) && len(u) < n; i += 2 {
			u = append(u, binary.LittleEndian.Uint16(b[i:]))
		}
	default:
		return "", false
	}
	s := string(utf16.Decode(u))
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return s, true
}

func (osMover) restore(it *Item) error {
	if it.Stored == "" {
		return errors.New("restore from the Recycle Bin in Explorer")
	}
	if err := os.Rename(it.Stored, it.Orig); err != nil {
		return err
	}
	os.Remove(it.Info)
	return nil
}

func (osMover) purge(it *Item) error {
	if it.Stored == "" {
		return errors.New("empty the Recycle Bin in Explorer")
	}
	if err := removeAll(it.Stored); err != nil {
		return err
	}
	os.Remove(it.Info)
	return nil
}
