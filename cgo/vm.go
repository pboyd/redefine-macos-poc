//go:build darwin && arm64

package cgo

/*
#include <mach/mach.h>
#include <mach/mach_vm.h>
*/
import "C"
import (
	"fmt"
	"unsafe"
)

type KernErr int

func (e KernErr) Error() string {
	// Error strings from https://web.mit.edu/darwin/src/modules/xnu/osfmk/man/vm_remap.html and kern_return.h
	switch e {
	case C.KERN_INVALID_ADDRESS:
		return "Specified address is not currently valid."
	case C.KERN_NO_SPACE:
		return "There is not enough space in the task's address space to allocate the new region for the memory object."
	case C.KERN_PROTECTION_FAILURE:
		return "Specified memory is valid, but the backing memory manager is not permitted by the requesting task."
	}
	return fmt.Sprintf("Unknown error code: %d", e)
}

// VmRemap makes a new virtual memory mapping of srcAddr. If addr is 0 then the
// new mapping will be allocated anywhere, otherwise the page will be requested
// at exactly the given address and may overwrite a previously existing mapping
// at that address.
func VmRemap(addr uintptr, srcAddr uintptr, size uintptr) (unsafe.Pointer, error) {
	var vmAddr C.mach_vm_address_t
	vmAddr = C.mach_vm_address_t(addr)

	var flags int
	if addr == 0 {
		flags |= C.VM_FLAGS_ANYWHERE
	} else {
		flags |= C.VM_FLAGS_FIXED | C.VM_FLAGS_OVERWRITE
	}

	var curProt, maxProt C.vm_prot_t

	ret := C.mach_vm_remap(
		C.mach_task_self_,
		&vmAddr,
		C.mach_vm_address_t(size),
		0,
		C.int(flags),
		C.mach_task_self_,
		C.mach_vm_address_t(srcAddr),
		0,
		&curProt,
		&maxProt,
		C.VM_INHERIT_NONE,
	)

	if ret != 0 {
		return nil, KernErr(ret)
	}

	return unsafe.Pointer(uintptr(vmAddr)), nil
}
