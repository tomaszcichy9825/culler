//go:build windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// SHFileOperationW constants from shellapi.h.
const (
	foDelete          = 0x0003
	fofSilent         = 0x0004
	fofNoConfirmation = 0x0010
	fofAllowUndo      = 0x0040
)

// shFileOpStruct mirrors SHFILEOPSTRUCTW. pFrom is a double-null-terminated
// list of names, not a plain string.
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

// SystemTrasher returns the platform's user-recoverable trash: the Recycle Bin.
func SystemTrasher() (Trasher, error) {
	dll, err := syscall.LoadDLL("shell32.dll")
	if err != nil {
		return nil, err
	}
	proc, err := dll.FindProc("SHFileOperationW")
	if err != nil {
		return nil, err
	}
	return recycleBin{fileOperation: proc}, nil
}

// recycleBin moves files to the Recycle Bin through the shell, which is the
// only supported way to produce an entry the user can restore from Explorer.
type recycleBin struct {
	fileOperation *syscall.Proc
}

// Trash recycles path. SHFileOperation does not report where the file landed,
// so the recovered path is empty and the journal cannot undo the deletion:
// recovery is the Recycle Bin's own restore. Undo of a recycled file is
// therefore unsupported on Windows, and _Rejected mode is the alternative for
// users who want in-app undo. The op engine understands the empty destination:
// undo reports such files as restorable only from the Recycle Bin itself, and
// the undo stack steps over batches made entirely of them rather than jamming
// on a batch nothing can reverse.
func (r recycleBin) Trash(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	from, err := syscall.UTF16FromString(abs)
	if err != nil {
		return "", err
	}
	from = append(from, 0)

	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent,
	}
	ret, _, _ := r.fileOperation.Call(uintptr(unsafe.Pointer(&op)))
	runtime.KeepAlive(from)
	if ret != 0 {
		return "", fmt.Errorf("recycle %s: SHFileOperation returned 0x%x", abs, ret)
	}
	if op.fAnyOperationsAborted != 0 {
		return "", fmt.Errorf("recycle %s: operation aborted", abs)
	}
	return "", nil
}
