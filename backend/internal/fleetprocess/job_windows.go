//go:build windows

// Package fleetprocess contains Fleet-owned process trees in a Windows job.
package fleetprocess

import (
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Contain pins provider descendants to their host, including children whose
// immediate parent exits first. Ordinary AO hosts retain their existing policy.
func Contain(pid int) (func(), error) {
	if os.Getenv("AO_FLEET_HOME") == "" {
		return func() {}, nil
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	child, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	defer windows.CloseHandle(child)
	if err := windows.AssignProcessToJobObject(job, child); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return sync.OnceFunc(func() { _ = windows.CloseHandle(job) }), nil
}
