package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"reflect"
	"syscall"
	"time"
	"unsafe"
)

/*
static void cacheflush(char *start, char *end) {
	__builtin___clear_cache(start, end);
}
*/
import "C"

func myTimeNow() time.Time {
	return time.Date(2026, 1, 30, 17, 0, 0, 0, time.FixedZone("Somewhere", -5))
}

func main() {
	err := redefineFunc(time.Now, myTimeNow)
	if err != nil {
		fmt.Fprintf(os.Stderr, "redefineFunc failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(time.Now().Format(time.Kitchen))
}

func redefineFunc[T any](fn T, newFn T) error {
	addr := reflect.ValueOf(fn).Pointer()
	buf := unsafe.Slice((*byte)(unsafe.Pointer(addr)), 4)

	err := mprotect(addr, len(buf), syscall.PROT_READ|syscall.PROT_WRITE|syscall.PROT_EXEC)
	if err != nil {
		return err
	}

	dest := reflect.ValueOf(newFn).Pointer() // Where to jump to
	offset := int32(dest - addr)

	// Encode the instruction:
	// -----------------------------------
	// | 000101 | ... 26 bit address ... |
	// -----------------------------------
	inst := (5 << 26) | (uint32(offset>>2) & (1<<26 - 1))
	binary.LittleEndian.PutUint32(buf, inst)

	err = mprotect(addr, len(buf), syscall.PROT_READ|syscall.PROT_EXEC)
	if err != nil {
		return err
	}

	cacheflush(buf)

	return nil
}

func mprotect(addr uintptr, length int, flags int) error {
	pageSize := syscall.Getpagesize()

	// Round address down to page boundary.
	pageStart := addr &^ (uintptr(syscall.Getpagesize()) - 1)

	// Round up to cover complete pages.
	regionSize := (int(addr-pageStart) + length + pageSize - 1) &^ (pageSize - 1)

	region := unsafe.Slice((*byte)(unsafe.Pointer(pageStart)), regionSize)
	return syscall.Mprotect(region, flags)
}

func cacheflush(buf []byte) {
	start := unsafe.Pointer(unsafe.SliceData(buf))
	end := unsafe.Pointer(uintptr(len(buf)) + uintptr(start))
	C.cacheflush((*C.char)(start), (*C.char)(end))
}
