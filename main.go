//go:build darwin && arm64

package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"reflect"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

/*
#include <pthread.h>
#include <mach/mach.h>
#include <mach/mach_vm.h>

static void cacheflush(char *start, char *end) {
	__builtin___clear_cache(start, end);
}
*/
import "C"

func myTimeNow() time.Time {
	return time.Date(2026, 1, 30, 17, 0, 0, 0, time.FixedZone("Somewhere", 0))
}

func main() {
	err := redefineFunc(time.Now, myTimeNow)
	if err != nil {
		log.Fatalf("redefineFunc failed: %v\n", err)
	}

	fmt.Println(time.Now().Format(time.Kitchen))
}

func redefineFunc[T any](fn, newFn T) error {
	writeOffset, err := getWritableText()
	if err != nil {
		return err
	}

	addr := reflect.ValueOf(fn).Pointer()
	buf := unsafe.Slice((*byte)(unsafe.Pointer(addr+writeOffset)), 4)

	dest := reflect.ValueOf(newFn).Pointer() // Where to jump to
	target := int32(dest - addr)

	// Encode the instruction:
	// -----------------------------------
	// | 000101 | ... 26 bit address ... |
	// -----------------------------------
	inst := (5 << 26) | (uint32(target>>2) & (1<<26 - 1))

	binary.LittleEndian.PutUint32(buf, inst)
	cacheflush(buf)

	return nil
}

func cacheflush(buf []byte) {
	start := unsafe.Pointer(unsafe.SliceData(buf))
	end := unsafe.Pointer(uintptr(len(buf)) + uintptr(start))
	C.cacheflush((*C.char)(start), (*C.char)(end))
}

var pageSize = uintptr(syscall.Getpagesize())
var pageMask = ^(pageSize - 1)
var writeOffset uintptr

func getWritableText() (uintptr, error) {
	if writeOffset != 0 {
		return writeOffset, nil
	}

	text := lastmoduledatap.text & pageMask
	etext := (lastmoduledatap.etext + pageSize - 1) & pageMask
	size := etext - text

	newText, err := unix.MmapPtr(-1, 0, nil, size,
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANON|unix.MAP_PRIVATE,
	)
	if err != nil {
		return 0, fmt.Errorf("mmap: %w", err)
	}

	src := unsafe.Slice((*byte)(unsafe.Pointer(text)), size)
	dest := unsafe.Slice((*byte)(newText), size)

	copy(dest, src)

	err = unix.Mprotect(dest, unix.PROT_READ|unix.PROT_EXEC)
	if err != nil {
		return 0, fmt.Errorf("mprotect r-x: %w", err)
	}

	_, err = vmRemap(text, uintptr(newText), size)
	if err != nil {
		return 0, fmt.Errorf("vmRemap: %w", err)
	}

	err = unix.Mprotect(dest, unix.PROT_READ|unix.PROT_WRITE)
	if err != nil {
		return 0, fmt.Errorf("mprotect rw-: %w", err)
	}

	writeOffset = uintptr(newText) - text

	return writeOffset, nil
}

type kernErr int

func (e kernErr) Error() string {
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

func vmRemap(addr uintptr, srcAddr uintptr, size uintptr) (unsafe.Pointer, error) {
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
		return nil, kernErr(ret)
	}

	return unsafe.Pointer(uintptr(vmAddr)), nil
}

//go:linkname lastmoduledatap runtime.lastmoduledatap
var lastmoduledatap *moduledata
