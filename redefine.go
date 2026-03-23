//go:build darwin && arm64

package main

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"syscall"
	"unsafe"

	"github.com/pboyd/redefine-macos-poc/cgo"
	"golang.org/x/arch/arm64/arm64asm"
	"golang.org/x/sys/unix"
)

var pageSize = uintptr(syscall.Getpagesize())
var pageMask = ^(pageSize - 1)

func redefineFunc[T any](fn T, newFn T) error {
	err := fork()
	if err != nil {
		return fmt.Errorf("fork: %w", err)
	}

	pc := reflect.ValueOf(fn).Pointer()
	if pc >= origText && pc < origEtext {
		pc += offset
	}

	targetPC := reflect.ValueOf(newFn).Pointer()
	if targetPC >= origText && targetPC < origEtext {
		targetPC += offset
	}

	// B has a 26-bit signed address, which should be more than enough, but just to be safe.
	relAddr := int64(targetPC) - int64(pc)
	if relAddr > (1<<27)-4 || relAddr < -(1<<27) {
		return fmt.Errorf("branch target out of range: %d bytes (limit 128 MB)", relAddr)
	}

	writeB(
		unsafe.Slice((*byte)(unsafe.Pointer(pc)), 4),
		int32(relAddr),
	)

	return nil
}

var writeB func([]byte, int32) = _writeB

func _writeB(buf []byte, relAddr int32) {
	cgo.JITWriteStart()

	// Encode the instruction:
	// -----------------------------------
	// | 000101 | ... 26 bit address ... |
	// -----------------------------------
	inst := (5 << 26) | (uint32(relAddr>>2) & (1<<26 - 1))
	binary.LittleEndian.PutUint32(buf, inst)

	cgo.ClearCache(buf)

	cgo.JITWriteEnd()
}

var (
	offset    uintptr
	origText  uintptr
	origEtext uintptr
)

//go:linkname lastmoduledatap runtime.lastmoduledatap
var lastmoduledatap *moduledata

func fork() error {
	// Duplicate the text section unless it's already done
	if offset == 0 {
		origText = lastmoduledatap.text
		origEtext = lastmoduledatap.etext

		var err error
		offset, err = duplicateText()
		if err != nil {
			return err
		}

		err = patchRodataCodePtrs(offset)
		if err != nil {
			return err
		}

		// writeB needs to point to the original TEXT section because we can't
		// execute from the new section at the same time that we write to it.
		writeB = offsetFunc(writeB, -offset)
	}

	if !runningInDuplicate() {
		for f := getFrame(); f != nil; f = f.next {
			if f.lr >= origText && f.lr < origEtext {
				f.lr += offset
			}
		}
	}

	return nil
}

var newModdata moduledata

func duplicateText() (offset uintptr, err error) {
	text := lastmoduledatap.text & pageMask

	// rodata is the first non-code address after all executable sections
	// (__text, __stubs) in the __TEXT segment.
	etext := (lastmoduledatap.rodata + pageSize - 1) & pageMask

	destPtr, err := unix.MmapPtr(
		-1, 0,
		unsafe.Pointer(lastmoduledatap.end),
		etext-text,
		unix.PROT_READ|unix.PROT_WRITE|unix.PROT_EXEC,
		unix.MAP_ANON|unix.MAP_PRIVATE|unix.MAP_JIT,
	)
	if err != nil {
		return 0, fmt.Errorf("mmap JIT text (%d bytes): %w", etext-text, err)
	}

	cgo.JITWriteStart()
	defer cgo.JITWriteEnd()

	src := unsafe.Slice((*byte)(unsafe.Pointer(text)), etext-text)
	dest := unsafe.Slice((*byte)(destPtr), etext-text)
	copy(dest, src)

	offset = uintptr(destPtr) - text

	err = fixADRP(dest, offset)
	if err != nil {
		return 0, fmt.Errorf("fixADRP: %w", err)
	}

	// Find the duplicate marker in src, then translate that address to dest and set the value to 1.
	*(*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(dupMarker())) + offset)) = 1

	cgo.ClearCache(dest)

	newModdata = *lastmoduledatap
	newModdata.text += offset
	newModdata.etext += offset
	newModdata.minpc += offset
	newModdata.maxpc += offset

	newPcHeader := *lastmoduledatap.pcHeader
	newPcHeader.textStart += offset
	newModdata.pcHeader = &newPcHeader

	newModdata.textsectmap = make([]textsect, len(lastmoduledatap.textsectmap))
	for i := range lastmoduledatap.textsectmap {
		newModdata.textsectmap[i] = lastmoduledatap.textsectmap[i]
		newModdata.textsectmap[i].baseaddr += offset
	}

	lastmoduledatap.next = &newModdata

	return offset, nil
}

const (
	// ADR/ADRP is encoded as:
	// --------------------------------------------------
	// | P | lo 2 bits | 10000 | hi 19 bits | 5-bit reg |
	// --------------------------------------------------
	// Mask for the address:
	adrAddressMask = uint32(3<<29 | 0x7ffff<<5)
)

func fixADRP(code []byte, offset uintptr) error {
	destBase := uintptr(unsafe.Pointer(unsafe.SliceData(code)))
	srcBase := destBase - offset

	// ADRP always uses 4KB page granularity regardless of OS page size.
	const adrpPageMask = ^uintptr(0xfff)
	origTextPage := origText & adrpPageMask
	origEtextPage := (origEtext + 0xfff) & adrpPageMask

	for i := uintptr(0); i < uintptr(len(code)); i += 4 {
		raw := code[i : i+4]
		inst, err := arm64asm.Decode(raw)
		if err != nil {
			// Just skip bad instructions. It's probably padding or data.
			continue
		}

		destPC := destBase + i
		srcPC := srcBase + i

		switch inst.Op {
		case arm64asm.ADRP:
			oldArg := int64(inst.Args[1].(arm64asm.PCRel))

			// Don't update the address if the target is within the
			// original text. We want those to keep the same relative value
			// so that they'll point to the new text.
			targetPage := uintptr(int64(srcPC&adrpPageMask) + oldArg)
			if targetPage >= origTextPage && targetPage < origEtextPage {
				continue
			}

			newImm := (int64(srcPC&adrpPageMask) + oldArg - int64(destPC&adrpPageMask)) >> 12
			if newImm < -(1<<20) || newImm >= (1<<20) {
				return fmt.Errorf("ADRP at byte offset %d: adjusted immediate %d out of 21-bit signed range", i, newImm)
			}
			newArg := uint32(newImm)

			encoded := binary.LittleEndian.Uint32(raw) &^ adrAddressMask
			encoded |= (newArg & 3) << 29             // Lowest 2 bits to bits 30 and 29
			encoded |= ((newArg >> 2) & 0x7ffff) << 5 // Highest 19 bits to bits 23 to 5
			binary.LittleEndian.PutUint32(raw, encoded)

		}
	}
	return nil
}

func patchRodataCodePtrs(offset uintptr) error {
	if lastmoduledatap.etext >= lastmoduledatap.noptrdata {
		return nil
	}

	mapStart := (lastmoduledatap.etext + pageSize - 1) & pageMask
	mapEnd := lastmoduledatap.noptrdata & pageMask
	if mapStart >= mapEnd {
		return nil
	}

	entries := make(map[uintptr]struct{}, len(lastmoduledatap.ftab))
	for _, ft := range lastmoduledatap.ftab {
		entries[lastmoduledatap.text+uintptr(ft.entryoff)] = struct{}{}
	}

	size := mapEnd - mapStart

	tmpPtr, err := unix.MmapPtr(-1, 0, nil, size,
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		return fmt.Errorf("mmap temp rodata (%d bytes): %w", size, err)
	}
	tmpSlice := unsafe.Slice((*byte)(tmpPtr), int(size))
	copy(tmpSlice, unsafe.Slice((*byte)(unsafe.Pointer(mapStart)), int(size)))

	// ignore pclntable area, because patching those pointers caused crashes.
	pclnStart := uintptr(unsafe.Pointer(unsafe.SliceData(lastmoduledatap.pclntable)))
	pclnEnd := pclnStart + uintptr(len(lastmoduledatap.pclntable))

	for addr := mapStart; addr+8 <= mapEnd; addr += 8 {
		if addr >= pclnStart && addr < pclnEnd {
			continue
		}

		off := addr - mapStart
		val := *(*uintptr)(unsafe.Pointer(&tmpSlice[off]))
		if val >= lastmoduledatap.text && val < lastmoduledatap.etext {
			if _, ok := entries[val]; ok {
				*(*uintptr)(unsafe.Pointer(&tmpSlice[off])) = val + offset
			}
		}
	}

	err = unix.Mprotect(tmpSlice, unix.PROT_READ|unix.PROT_EXEC)
	if err != nil {
		unix.MunmapPtr(tmpPtr, size)
		return fmt.Errorf("mprotect temp to r-x: %w", err)
	}

	_, err = cgo.VmRemap(mapStart, uintptr(tmpPtr), size)
	if err != nil {
		unix.MunmapPtr(tmpPtr, size)
		return fmt.Errorf("vm_remap rodata (%d bytes at %#x): %w", size, mapStart, err)
	}

	unix.MunmapPtr(tmpPtr, size)

	return nil
}

type frame struct {
	next *frame
	lr   uintptr
}

func getFrame() *frame

func dupMarker() *uint32

func runningInDuplicate() bool {
	return *dupMarker() != 0
}

var refs []any

// offsetFunc takes the address of fn and adds offset to it, then derefs that
// address as a function of the same type.
func offsetFunc[T any](fn T, offset uintptr) T {
	fnv := reflect.ValueOf(fn)
	if fnv.Kind() != reflect.Func {
		panic("not a function")
	}

	ptr := new(uintptr)
	*ptr = fnv.Pointer() + offset
	refs = append(refs, ptr)

	return *(*T)(unsafe.Pointer(&ptr))
}
