//go:build darwin && arm64 && go1.26

package main

// moduledata records information about the layout of the executable
// image. It is written by the linker. Any changes here must be
// matched changes to the code in cmd/link/internal/ld/symtab.go:symtab.
// moduledata is stored in statically allocated non-pointer memory;
// none of the pointers here are visible to the garbage collector.
type moduledata struct {
	pcHeader     *pcHeader
	funcnametab  []byte
	cutab        []uint32
	filetab      []byte
	pctab        []byte
	pclntable    []byte
	ftab         []functab
	findfunctab  uintptr
	minpc, maxpc uintptr

	text, etext           uintptr
	noptrdata, enoptrdata uintptr
	data, edata           uintptr
	bss, ebss             uintptr
	noptrbss, enoptrbss   uintptr
	covctrs, ecovctrs     uintptr
	end, gcdata, gcbss    uintptr
	types, etypes         uintptr
	rodata                uintptr
	gofunc                uintptr // go.func.*
	epclntab              uintptr

	textsectmap []textsect

	// The following fields exist in the runtime struct but are not used by
	// this package. They are included here to correctly place the next field
	// at the same offset as in the runtime's moduledata struct.
	_typelinks    [3]uintptr // []int32
	_itablinks    [3]uintptr // []*itab
	_ptab         [3]uintptr // []ptabEntry
	_pluginpath   [2]uintptr // string
	_pkghashes    [3]uintptr // []modulehash
	_inittasks    [3]uintptr // []*initTask
	_modulename   [2]uintptr // string
	_modulehashes [3]uintptr // []modulehash
	_hasmain      uint8
	_bad          bool
	_             [6]byte    // padding to align the following bitvectors
	_gcdatamask   [2]uintptr // bitvector
	_gcbssmask    [2]uintptr // bitvector
	_typemap      uintptr    // map[typeOff]*_type (a pointer)

	next *moduledata
}

// pcHeader holds data used by the pclntab lookups.
type pcHeader struct {
	magic          uint32  // 0xFFFFFFF1
	pad1, pad2     uint8   // 0,0
	minLC          uint8   // min instruction size
	ptrSize        uint8   // size of a ptr in bytes
	nfunc          int     // number of functions in the module
	nfiles         uint    // number of entries in the file tab
	textStart      uintptr // base for function entry PC offsets in this module, equal to moduledata.text
	funcnameOffset uintptr // offset to the funcnametab variable from pcHeader
	cuOffset       uintptr // offset to the cutab variable from pcHeader
	filetabOffset  uintptr // offset to the filetab variable from pcHeader
	pctabOffset    uintptr // offset to the pctab variable from pcHeader
	pclnOffset     uintptr // offset to the pclntab variable from pcHeader
}

type functab struct {
	entryoff uint32 // relative to runtime.text
	funcoff  uint32
}

type textsect struct {
	vaddr    uintptr // prelinked section vaddr
	end      uintptr // vaddr + section length
	baseaddr uintptr // relocated section address
}
