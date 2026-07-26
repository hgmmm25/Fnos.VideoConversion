package winapi

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type JobHandle uintptr

var (
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	createJobObjectProc          = kernel32.NewProc("CreateJobObjectW")
	setInformationJobObjectProc  = kernel32.NewProc("SetInformationJobObject")
	assignProcessToJobObjectProc = kernel32.NewProc("AssignProcessToJobObject")
	terminateJobObjectProc       = kernel32.NewProc("TerminateJobObject")
	closeHandleProc              = kernel32.NewProc("CloseHandle")
	createMutexProc              = kernel32.NewProc("CreateMutexW")
	releaseMutexProc             = kernel32.NewProc("ReleaseMutex")
	getDiskFreeSpaceExProc       = kernel32.NewProc("GetDiskFreeSpaceExW")
	globalMemoryStatusExProc     = kernel32.NewProc("GlobalMemoryStatusEx")
)

var (
	advapi32            = windows.NewLazySystemDLL("advapi32.dll")
	regOpenKeyExProc    = advapi32.NewProc("RegOpenKeyExW")
	regCloseKeyProc     = advapi32.NewProc("RegCloseKey")
	regSetValueExProc   = advapi32.NewProc("RegSetValueExW")
	regDeleteValueProc  = advapi32.NewProc("RegDeleteValueW")
	regQueryValueExProc = advapi32.NewProc("RegQueryValueExW")
)

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	messageBoxProc            = user32.NewProc("MessageBoxW")
	getSystemMetricsProc      = user32.NewProc("GetSystemMetrics")
	systemParametersInfoWProc = user32.NewProc("SystemParametersInfoW")
	findWindowWProc           = user32.NewProc("FindWindowW")
	setWindowPosProc          = user32.NewProc("SetWindowPos")
)

var (
	kernel32Sleep               = windows.NewLazySystemDLL("kernel32.dll")
	setThreadExecutionStateProc = kernel32Sleep.NewProc("SetThreadExecutionState")
)

const (
	esContinuous       uint32 = 0x80000000
	esSystemRequired   uint32 = 0x00000001
	esAwaymodeRequired uint32 = 0x00000040
)

// PreventSleep 阻止系统进入休眠状态（保持 CPU/系统活动），可重复调用确保生效。
func PreventSleep() {
	_, _, _ = setThreadExecutionStateProc.Call(uintptr(esContinuous | esSystemRequired))
}

// AllowSleep 恢复系统默认的休眠行为。
func AllowSleep() {
	_, _, _ = setThreadExecutionStateProc.Call(uintptr(esContinuous))
}

type JOBOBJECT_EXTENDED_LIMIT_INFORMATION struct {
	BasicLimitInformation JOBOBJECT_BASIC_LIMIT_INFORMATION
	IoInfo                IO_COUNTERS
	ProcessMemoryLimit    uint64
	JobMemoryLimit        uint64
	PeakProcessMemoryUsed uint64
	PeakJobMemoryUsed     uint64
}

type JOBOBJECT_BASIC_LIMIT_INFORMATION struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uint64
	MaximumWorkingSetSize   uint64
	ActiveProcessLimit      uint32
	Affinity                uint64
	PriorityClass           uint32
	SchedulingClass         uint32
}

type IO_COUNTERS struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type MEMORYSTATUSEX struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

const (
	JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x2000
)

func CreateJobObject() (JobHandle, error) {
	handle, _, err := createJobObjectProc.Call(
		uintptr(0),
		uintptr(0),
	)

	if handle == 0 {
		return 0, err
	}

	extendedInfo := JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}

	_, _, err = setInformationJobObjectProc.Call(
		uintptr(handle),
		uintptr(9),
		uintptr(unsafe.Pointer(&extendedInfo)),
		uintptr(unsafe.Sizeof(extendedInfo)),
	)

	return JobHandle(handle), nil
}

func AssignProcessToJob(jobHandle JobHandle, processID int) error {
	process, err := os.FindProcess(processID)
	if err != nil {
		return err
	}

	_, _, err = assignProcessToJobObjectProc.Call(
		uintptr(jobHandle),
		uintptr(process.Pid),
	)

	if err != nil && err.Error() != "The operation completed successfully." {
		return err
	}

	return nil
}

func TerminateJob(jobHandle JobHandle) error {
	_, _, err := terminateJobObjectProc.Call(
		uintptr(jobHandle),
		uintptr(1),
	)

	if err != nil && err.Error() != "The operation completed successfully." {
		return err
	}

	return nil
}

func CloseJobObject(jobHandle JobHandle) error {
	_, _, err := closeHandleProc.Call(
		uintptr(jobHandle),
	)

	if err != nil && err.Error() != "The operation completed successfully." {
		return err
	}

	return nil
}

var mutexHandle uintptr

func IsAnotherInstanceRunning(mutexName string) bool {
	mutexNameUTF16, _ := syscall.UTF16PtrFromString(mutexName)
	handle, _, err := createMutexProc.Call(
		uintptr(0),
		uintptr(1),
		uintptr(unsafe.Pointer(mutexNameUTF16)),
	)

	if handle == 0 {
		return true
	}

	mutexHandle = handle

	return err != nil && err.Error() != "The operation completed successfully."
}

func SetAutoRunOnBoot(enable bool, exePath string) error {
	keyPath := `Software\Microsoft\Windows\CurrentVersion\Run`
	key, err := syscall.UTF16PtrFromString(keyPath)
	if err != nil {
		return err
	}

	var hKey windows.Handle
	err = windows.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, key, 0, syscall.KEY_ALL_ACCESS, &hKey)
	if err != nil {
		return err
	}
	defer windows.RegCloseKey(hKey)

	valueName, err := syscall.UTF16PtrFromString("Fnos.VC_Service")
	if err != nil {
		return err
	}

	if enable {
		value, err := syscall.UTF16PtrFromString(exePath)
		if err != nil {
			return err
		}

		_, _, err = regSetValueExProc.Call(
			uintptr(hKey),
			uintptr(unsafe.Pointer(valueName)),
			uintptr(0),
			uintptr(syscall.REG_SZ),
			uintptr(unsafe.Pointer(value)),
			uintptr(len(exePath)*2+2),
		)
		if err != nil && err.Error() != "The operation completed successfully." {
			return err
		}
	} else {
		_, _, err = regDeleteValueProc.Call(
			uintptr(hKey),
			uintptr(unsafe.Pointer(valueName)),
		)
		if err != nil && err != syscall.ERROR_FILE_NOT_FOUND && err.Error() != "The operation completed successfully." {
			return err
		}
	}

	return nil
}

func IsAutoRunOnBootEnabled() bool {
	keyPath := `Software\Microsoft\Windows\CurrentVersion\Run`
	key, err := syscall.UTF16PtrFromString(keyPath)
	if err != nil {
		return false
	}

	var hKey windows.Handle
	err = windows.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, key, 0, syscall.KEY_READ, &hKey)
	if err != nil {
		return false
	}
	defer windows.RegCloseKey(hKey)

	valueName, err := syscall.UTF16PtrFromString("Fnos.VC_Service")
	if err != nil {
		return false
	}

	var buf [512]uint16
	var bufSize uint32 = uint32(len(buf))
	_, _, err = regQueryValueExProc.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(valueName)),
		uintptr(0),
		uintptr(0),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if err != nil && err.Error() != "The operation completed successfully." {
		return false
	}

	return true
}

func FindFreePort(startPort, endPort int) (int, error) {
	for port := startPort; port <= endPort; port++ {
		addr := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			listener.Close()
			return port, nil
		}
	}

	return 0, fmt.Errorf("no free port found in range %d-%d", startPort, endPort)
}

func ShowMessageBox(message, title string, style uint32) {
	msg, _ := syscall.UTF16PtrFromString(message)
	tit, _ := syscall.UTF16PtrFromString(title)

	go func() {
		messageBoxProc.Call(0, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(tit)), uintptr(style))
	}()
}

func ShowInfoMessage(message string) {
	ShowMessageBox(message, "FVCS Info", 0x00000040)
}

func ShowErrorMessage(message string) {
	ShowMessageBox(message, "FVCS Error", 0x00000010)
}

func ShowConfirmMessage(message string) bool {
	msg, _ := syscall.UTF16PtrFromString(message)
	tit, _ := syscall.UTF16PtrFromString("FVCS Confirm")

	ret, _, _ := messageBoxProc.Call(0, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(tit)), uintptr(0x00000004))
	return ret == 6
}

func IsDarkMode() bool {
	keyPath := `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	key, err := syscall.UTF16PtrFromString(keyPath)
	if err != nil {
		return false
	}

	var hKey windows.Handle
	err = windows.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, key, 0, syscall.KEY_READ, &hKey)
	if err != nil {
		return false
	}
	defer windows.RegCloseKey(hKey)

	valueName, err := syscall.UTF16PtrFromString("AppsUseLightTheme")
	if err != nil {
		return false
	}

	var value uint32
	var bufSize uint32 = 4
	_, _, err = regQueryValueExProc.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(valueName)),
		uintptr(0),
		uintptr(0),
		uintptr(unsafe.Pointer(&value)),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if err != nil && err.Error() != "The operation completed successfully." {
		return false
	}

	return value == 0
}

func GetDiskFreeSpace(path string) (int64, error) {
	var freeBytesAvailable uint64
	var totalNumberOfBytes uint64
	var totalNumberOfFreeBytes uint64

	pathPtr := windows.StringToUTF16Ptr(path)
	_, _, err := getDiskFreeSpaceExProc.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)

	if err != nil && err.Error() != "The operation completed successfully." {
		return 0, err
	}

	return int64(freeBytesAvailable), nil
}

func StartProcess(exePath string) error {
	workingDir := filepath.Dir(exePath)
	exePathPtr, _ := syscall.UTF16PtrFromString(exePath)
	workingDirPtr, _ := syscall.UTF16PtrFromString(workingDir)

	var si syscall.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))

	var pi syscall.ProcessInformation

	err := syscall.CreateProcess(
		nil,
		exePathPtr,
		nil,
		nil,
		false,
		windows.CREATE_NO_WINDOW,
		nil,
		workingDirPtr,
		&si,
		&pi,
	)
	if err != nil {
		return fmt.Errorf("CreateProcess failed: %v", err)
	}

	syscall.CloseHandle(pi.Thread)
	syscall.CloseHandle(pi.Process)

	return nil
}

func GetMemoryInfo() (uint64, uint64, error) {
	var memInfo MEMORYSTATUSEX
	memInfo.Length = uint32(unsafe.Sizeof(memInfo))

	_, _, err := globalMemoryStatusExProc.Call(uintptr(unsafe.Pointer(&memInfo)))
	if err != nil && err.Error() != "The operation completed successfully." {
		return 0, 0, err
	}

	return memInfo.TotalPhys, memInfo.AvailPhys, nil
}

const (
	SM_CXSCREEN = 0
	SM_CYSCREEN = 1
)

type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

const (
	SPI_GETWORKAREA = 0x0030
)

func GetScreenWorkArea() (int32, int32, int32, int32) {
	var rc RECT
	ret, _, _ := systemParametersInfoWProc.Call(
		uintptr(SPI_GETWORKAREA),
		0,
		uintptr(unsafe.Pointer(&rc)),
		0,
	)
	if ret == 0 {
		cx, _, _ := getSystemMetricsProc.Call(uintptr(SM_CXSCREEN))
		cy, _, _ := getSystemMetricsProc.Call(uintptr(SM_CYSCREEN))
		return 0, 0, int32(cx), int32(cy)
	}
	return rc.Left, rc.Top, rc.Right, rc.Bottom
}

func GetBottomRightPosition(winWidth, winHeight int) (int, int) {
	_, _, right, bottom := GetScreenWorkArea()
	x := int(right) - winWidth - 10
	y := int(bottom) - winHeight - 10
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

const (
	SWP_NOSIZE     = 0x0001
	SWP_NOZORDER   = 0x0004
	SWP_SHOWWINDOW = 0x0040
)

func SetWindowPosition(title string, x, y int) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	hwnd, _, _ := findWindowWProc.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return
	}
	setWindowPosProc.Call(
		hwnd,
		0,
		uintptr(x),
		uintptr(y),
		0,
		0,
		uintptr(SWP_NOSIZE|SWP_NOZORDER),
	)
}
