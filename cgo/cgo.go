//go:build darwin && arm64

package cgo

/*
#include <pthread.h>

static void cacheflush(char *start, char *end) {
	__builtin___clear_cache(start, end);
}
*/
import "C"
import (
	"runtime"
	"unsafe"
)

func ClearCache(buf []byte) {
	start := unsafe.Pointer(unsafe.SliceData(buf))
	end := unsafe.Pointer(uintptr(len(buf)) + uintptr(start))
	C.cacheflush((*C.char)(start), (*C.char)(end))
}

func JITWriteStart() {
	runtime.LockOSThread()
	C.pthread_jit_write_protect_np(0)
}

func JITWriteEnd() {
	C.pthread_jit_write_protect_np(1)
	runtime.UnlockOSThread()
}
