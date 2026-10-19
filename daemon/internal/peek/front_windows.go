//go:build windows

package peek

// The app in front on Windows: the foreground window's process, its program's path. No title.
// Quiet: what Windows itself holds its notifications for (full screen, a presentation).

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var shQueryUserNotificationState = windows.NewLazySystemDLL("shell32.dll").NewProc("SHQueryUserNotificationState")

func quietNow() bool {
	if shQueryUserNotificationState.Find() != nil {
		return false
	}
	var state int32
	if hr, _, _ := shQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&state))); hr != 0 {
		return false
	}
	switch state {
	case 1, 2, 3, 4, 7: // not present, busy (full screen), Direct3D full screen, presentation mode, a full-screen store app
		return true
	}
	return false
}

func NewSampler() (Sampler, string) { return windowsSampler{}, ModeFront }

type windowsSampler struct{}

func (windowsSampler) Front() (Front, error) {
	hwnd := windows.GetForegroundWindow()
	if hwnd == 0 {
		return Front{}, nil
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid == 0 {
		return Front{}, nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return Front{}, nil // an elevated or protected process: unknown
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return Front{}, nil
	}
	return Front{Exe: windows.UTF16ToString(buf[:n]), Quiet: quietNow()}, nil
}

func (windowsSampler) Close() {}
