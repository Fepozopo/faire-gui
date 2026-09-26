//go:build windows

package application

import (
	"context"
	"fmt"
	"runtime"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// Explorer-style UI and existence checks keep the picker limited to files that can be opened.
	ofExplorer         = 0x00080000
	ofFileMustExist    = 0x00001000
	ofPathMustExist    = 0x00000800
	maxFilePath        = 32768
	openFilenameSize64 = 152
)

var (
	// NewLazySystemDLL loads only the trusted system comdlg32.dll, which provides Windows' native Open dialog.
	commonDialogDLL      = windows.NewLazySystemDLL("comdlg32.dll")
	getOpenFileName      = commonDialogDLL.NewProc("GetOpenFileNameW")
	commDlgExtendedError = commonDialogDLL.NewProc("CommDlgExtendedError")
)

// openFilename matches the Windows OPENFILENAMEW layout passed to GetOpenFileNameW.
// Pointer-sized fields and their order must match the Win32 ABI on both supported 64-bit Windows targets.
type openFilename struct {
	structSize    uint32
	owner         windows.Handle
	instance      windows.Handle
	filter        *uint16
	customFilter  *uint16
	maxCustom     uint32
	filterIndex   uint32
	file          *uint16
	maxFile       uint32
	fileTitle     *uint16
	maxFileTitle  uint32
	initialDir    *uint16
	title         *uint16
	flags         uint32
	fileOffset    uint16
	fileExtension uint16
	defaultExt    *uint16
	customData    uintptr
	hook          uintptr
	templateName  *uint16
	reserved      uintptr
	reservedSize  uint32
	extendedFlags uint32
}

// These compile-time checks keep the Win32 structure at its documented size on both supported 64-bit targets.
var _ [openFilenameSize64 - unsafe.Sizeof(openFilename{})]byte
var _ [unsafe.Sizeof(openFilename{}) - openFilenameSize64]byte

// chooseCSVFile displays Windows' Explorer-style native Open dialog without starting a console process.
// ctx prevents opening after shutdown; it returns the chosen path or an error for cancellation or dialog failure.
func chooseCSVFile(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// The Win32 dialog runs its own modal message loop, so keep it on one OS thread until it closes.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	title, err := windows.UTF16PtrFromString("Select a CSV file")
	if err != nil {
		return "", err
	}
	// OPENFILENAMEW expects a double-NUL-terminated list of display-name/pattern pairs.
	filter := utf16.Encode([]rune("CSV files (*.csv)\x00*.csv\x00All files (*.*)\x00*.*\x00\x00"))
	file := make([]uint16, maxFilePath)
	dialog := openFilename{
		structSize: uint32(unsafe.Sizeof(openFilename{})),
		filter:     &filter[0],
		file:       &file[0],
		maxFile:    uint32(len(file)),
		title:      title,
		flags:      ofExplorer | ofFileMustExist | ofPathMustExist,
	}
	// GetOpenFileNameW writes the selected UTF-16 path into file and returns zero on cancel or error.
	selected, _, _ := getOpenFileName.Call(uintptr(unsafe.Pointer(&dialog)))
	runtime.KeepAlive(dialog)
	runtime.KeepAlive(filter)
	runtime.KeepAlive(title)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if selected == 0 {
		// CommDlgExtendedError distinguishes a normal cancellation (zero) from a dialog failure.
		code, _, _ := commDlgExtendedError.Call()
		if code != 0 {
			return "", fmt.Errorf("Windows file picker failed (dialog code %#x)", code)
		}
		return "", fmt.Errorf("file selection canceled")
	}
	return windows.UTF16ToString(file), nil
}
